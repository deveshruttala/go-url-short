#!/bin/bash

set -e

sudo mkdir -p /app
sudo chown -R ec2-user:ec2-user /app

sudo amazon-linux-extras enable epel
sudo yum clean metadata; sudo yum -y install epel-release; sudo yum update -y

sudo yum install -y git memcached go nginx certbot certbot-nginx

# pip3 install -U supervisor 

mkdir -p /tmp/{log,run}

sudo systemctl start memcached

## install sqlite from source
echo "install sqlite from source. 3.47.1"

wget https://www.sqlite.org/2024/sqlite-autoconf-3470100.tar.gz
tar sqlite-autoconf-3470100.tar.gz
cd sqlite-autoconf && \
	./configure && \
	make && sudo make install

echo "fetching source code"

cd app && \
	git clone https://github.com/ikouchiha47/shortner.git && \
	cd shortner && \
	go env -w GOPATH=/app/go; go env -w GOMODCACHE=/app/go/pkg/mod && \
	make build.all

echo "make sure your .env is up to date"

if [[ ! -f ".env" ]]; then
	echo "you need to setup your .env"
	echo "otherwise re run"
	echo "supervisord -c /path/to/supervisord.conf"
	exit 1
fi


sudo certbot --nginx -d shrtn.cloud

sudo cp /app/shortner/shortner.nginx.conf /etc/nginx/nginx.conf
sudo certbot --nginx -d shrtn.cloud

sudo cp /app/shortner/systemd/certbot.timer /etc/systemd/system/
sudo cp /app/shortner/systemd/certbot.service /etc/systemd/system/
sudo cp /app/shortner/systemd/shrtnr.service /etc/systemd/system/
sudo cp /app/shortner/systemd/syncs3.service /etc/systemd/system/
sudo cp /app/shortner/systemd/syncs3.timer /etc/systemd/system/
sudo cp /app/shortner/systemd/refiller.service /etc/systemd/system/
sudo cp /app/shortner/systemd/refiller.timer /etc/systemd/system/


sudo systemctl daemon-reload
sudo systemctl start certbot.timer
sudo systemctl start shrtnr.service
sudo systemctl start syncs3.timer
sudo systemctl start refiller.timer


curl localhost:9091
