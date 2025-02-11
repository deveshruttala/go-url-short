package watchers

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-batteries/shortner/app/db"
	"github.com/go-batteries/slicendice"
)

type shard struct {
	id string
}

func (s shard) ID() string {
	return db.DefaultSqliteDBNameBuilder(s.id)
}

type SqliteSyncer struct {
	databases []shard
}

func NewDBSyncer(shards []string) *SqliteSyncer {
	shards = append(shards, "shard_coordinator")

	return &SqliteSyncer{databases: slicendice.Map(shards, func(s string, _ int) shard {
		return shard{id: s}
	})}
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	return nil
}

func (w *SqliteSyncer) Run(ctx context.Context) {
	var bucketName = os.Getenv("BUCKET_NAME")
	var awsAccessKey = os.Getenv("AWS_ACCESS_KEY")
	var awsSecretKey = os.Getenv("AWS_SECRET_KEY")
	var awsRegion = os.Getenv("AWS_REGION")

	if awsAccessKey == "" || awsSecretKey == "" || awsRegion == "" {
		log.Printf("aws keys are missing. skipping sync")
		return
	}

	for _, shard := range w.databases {
		fileName := fmt.Sprintf("%s.db", shard.ID())
		backupFileName := fmt.Sprintf("backup_%s.db", shard.ID())

		cfg, err := config.LoadDefaultConfig(
			ctx,
			config.WithRegion(awsRegion),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(awsAccessKey, awsSecretKey, "")),
		)
		if err != nil {
			log.Printf("unable to load SDK config, %v", err)
			continue
		}

		client := s3.NewFromConfig(cfg)

		err = copyFile(fileName, backupFileName)
		if err != nil {
			log.Printf("unable to copy file %s", fileName)
			continue
		}

		// Open the SQLite file
		file, err := os.Open(backupFileName)
		if err != nil {
			log.Printf("failed to open file, %v", err)
			continue
		}
		defer file.Close()

		s3Key := fmt.Sprint("%s_%s.db", shard.ID(), time.Now().UTC().Format("2006_01_02"))

		_, err = client.PutObject(context.TODO(), &s3.PutObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(s3Key),
			Body:   file,
		})

		if err := os.Remove(backupFileName); err != nil {
			log.Printf("failed to remove file %s", backupFileName)
		}

		if err != nil {
			log.Printf("failed to upload file, %v", err)
			continue
		}

		log.Printf("File %s successfully uploaded to bucket %s\n", fileName, bucketName)
	}

}
