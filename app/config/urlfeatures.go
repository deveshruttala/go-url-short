package config

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/likexian/whois"
	whoisparser "github.com/likexian/whois-parser"
)

// Options struct to enable or disable specific heuristics
type URLCheckerOptions struct {
	CheckLength             bool
	CheckCharToNumberRatio  bool
	CheckSpecialCharCount   bool
	CheckIPBasedURL         bool
	CheckSuspiciousKeywords bool
	CheckSubdomainCount     bool
	CheckDomainAge          bool
	MaxURLLength            int
	MaxSubdomains           int
	MaxCharToNumberRatio    float64
	MaxSpecialCharCount     int
	Keywords                []string
	MinDomainAgeDays        int
	CheckSSL                bool
}

// URLChecker contains the options and rules
type URLChecker struct {
	options *URLCheckerOptions
}

// DefaultOptions provides default thresholds
func DefaultOptions() *URLCheckerOptions {
	return &URLCheckerOptions{
		CheckLength:             true,
		CheckCharToNumberRatio:  true,
		CheckSpecialCharCount:   true,
		CheckIPBasedURL:         true,
		CheckSuspiciousKeywords: true,
		CheckSubdomainCount:     true,
		CheckDomainAge:          false,
		MaxURLLength:            63,
		MaxSubdomains:           3,
		MaxCharToNumberRatio:    5.0,
		MaxSpecialCharCount:     10,
		Keywords:                []string{"free", "win", "offer", "prize", "localhost"},
		MinDomainAgeDays:        30,
		CheckSSL:                true,
	}
}

const (
	MaxIssues       = 8
	CutoffMaxIssues = 3
)

// WithOpts allows customization of URLCheckerOptions
type WithOpts func(opts *URLCheckerOptions)

func WithCheckLength(check bool) WithOpts {
	return func(opts *URLCheckerOptions) {
		opts.CheckLength = check
	}
}

func NewURLChecker(opts *URLCheckerOptions) *URLChecker {
	return &URLChecker{options: opts}
}

// ValidateURL applies all selected checks to a given URL
func (checker URLChecker) ValidateURL(inputURL string) ([]string, error) {
	parsed, err := url.Parse(inputURL)
	if err != nil {
		return []string{"Invalid URL format"}, errors.New("fuck him")
	}

	if parsed.Hostname() == "" || parsed.Scheme == "" {
		return []string{"Invalid URL format"}, errors.New("fuck golang, invalid url")
	}

	var issues []string

	// Check URL Length
	if checker.options.CheckLength && len(inputURL) > checker.options.MaxURLLength {
		issues = append(issues, fmt.Sprintf("URL is too long: %d characters", len(inputURL)))
	}

	// Check Char-to-Number Ratio
	if checker.options.CheckCharToNumberRatio {
		ratio := charToNumberRatio(parsed.Host)
		if ratio > checker.options.MaxCharToNumberRatio {
			issues = append(issues, fmt.Sprintf("Character-to-number ratio is too high: %.2f", ratio))
		}
	}

	// Check Special Character Count
	if checker.options.CheckSpecialCharCount {
		specialCharCount := countSpecialCharacters(inputURL)
		if specialCharCount > checker.options.MaxSpecialCharCount {
			issues = append(issues, fmt.Sprintf("Excessive special characters: %d", specialCharCount))
		}
	}

	// Check for IP-Based URL
	if checker.options.CheckIPBasedURL && isIPBasedURL(parsed.Host) {
		issues = append(issues, "URL uses an IP address instead of a domain name")
	}

	// Check for Suspicious Keywords
	if checker.options.CheckSuspiciousKeywords && containsKeywords(parsed.Host+parsed.Path, checker.options.Keywords) {
		issues = append(issues, "URL contains suspicious keywords")
	}

	// Check Subdomain Count
	if checker.options.CheckSubdomainCount {
		subdomainCount := countSubdomains(parsed.Host)
		if subdomainCount > checker.options.MaxSubdomains {
			issues = append(issues, fmt.Sprintf("Too many subdomains: %d", subdomainCount))
		}
	}

	// Check Domain Age (WHOIS required)
	if checker.options.CheckDomainAge {
		whoisInfo, err := getWHOISInfo(parsed.Hostname())

		if err != nil {
			issues = append(issues, fmt.Sprintf("WHOIS error: %v", err))
		} else {
			if whoisInfo.DomainAgeDays < checker.options.MinDomainAgeDays {
				issues = append(issues, fmt.Sprintf("Domain is too new: %d days old", whoisInfo.DomainAgeDays))
			}
			if time.Until(whoisInfo.ExpirationDate).Hours() < 30*24 {
				issues = append(issues, "Domain expires in less than 30 days")
			}
		}
	}

	if checker.options.CheckSSL {
		sslInfo, err := validateSSL(parsed.Hostname())
		if err != nil {
			issues = append(issues, fmt.Sprintf("SSL error: %v", err))
		} else {
			if !sslInfo.IsValid {
				issues = append(issues, "SSL certificate is not valid")
			}
			if !sslInfo.HostnameMatch {
				issues = append(issues, "SSL certificate hostname does not match")
			}
			if sslInfo.ExpirationDays < 30 {
				issues = append(issues, fmt.Sprintf("SSL certificate expires in %d days", sslInfo.ExpirationDays))
			}
		}
	}

	return issues, nil
}

// Helper Functions

// charToNumberRatio calculates the ratio of characters to numbers in a string
func charToNumberRatio(s string) float64 {
	numDigits := 0
	numChars := 0

	for _, r := range s {
		if r >= '0' && r <= '9' {
			numDigits++
		} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			numChars++
		}
	}

	if numDigits == 0 {
		return 0
	}
	return float64(numDigits) / float64(numChars)
}

// countSpecialCharacters counts the number of special characters in a URL
func countSpecialCharacters(s string) int {
	specialCharRegex := regexp.MustCompile(`[!@#\$%\^&\*\(\)_\+\-=\[\]\{\}\\|;:'",<>\?/]+`)
	return len(specialCharRegex.FindAllString(s, -1))
}

// isIPBasedURL checks if the hostname is an IP address
func isIPBasedURL(host string) bool {
	ipRegex := regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`)
	return ipRegex.MatchString(host)
}

// containsKeywords checks if a string contains any of the suspicious keywords
func containsKeywords(s string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(strings.ToLower(s), keyword) {
			return true
		}
	}
	return false
}

// countSubdomains counts the number of subdomains in a hostname
func countSubdomains(host string) int {
	parts := strings.Split(host, ".")
	return len(parts) - 2 // Exclude the main domain and TLD
}

type WHOISInfo struct {
	CreationDate   time.Time
	ExpirationDate time.Time
	Status         []string
	DomainAgeDays  int
}

func getWHOISInfo(domain string) (*WHOISInfo, error) {
	// Perform WHOIS query
	rawWhois, err := whois.Whois(domain)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch WHOIS data: %v", err)
	}

	// Parse WHOIS data
	parsed, err := whoisparser.Parse(rawWhois)
	if err != nil {
		return nil, fmt.Errorf("failed to parse WHOIS data: %v", err)
	}

	// Extract creation and expiration dates
	creationDate, err := time.Parse("2006-01-02", parsed.Domain.CreatedDate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse creation date: %v", err)
	}

	expirationDate, err := time.Parse("2006-01-02", parsed.Domain.ExpirationDate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse expiration date: %v", err)
	}

	// Calculate domain age
	domainAgeDays := int(time.Since(creationDate).Hours() / 24)

	return &WHOISInfo{
		CreationDate:   creationDate.UTC(),
		ExpirationDate: expirationDate.UTC(),
		Status:         parsed.Domain.Status,
		DomainAgeDays:  domainAgeDays,
	}, nil
}

type SSLInfo struct {
	Issuer         string
	ValidFrom      time.Time
	ValidUntil     time.Time
	IsValid        bool
	HostnameMatch  bool
	ExpirationDays int
}

// validateSSL checks the SSL certificate for a given hostname
func validateSSL(hostname string) (*SSLInfo, error) {
	conn, err := tls.Dial("tcp", hostname+":443", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %v", hostname, err)
	}
	defer conn.Close()

	cert := conn.ConnectionState().PeerCertificates[0]

	// Check validity and hostname match
	now := time.Now()
	isValid := now.After(cert.NotBefore) && now.Before(cert.NotAfter)
	hostnameMatch := cert.VerifyHostname(hostname) == nil
	expirationDays := int(cert.NotAfter.Sub(now).Hours() / 24)

	return &SSLInfo{
		Issuer:         cert.Issuer.CommonName,
		ValidFrom:      cert.NotBefore,
		ValidUntil:     cert.NotAfter,
		IsValid:        isValid,
		HostnameMatch:  hostnameMatch,
		ExpirationDays: expirationDays,
	}, nil
}
