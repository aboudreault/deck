package utils

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kong/deck/konnect"
	"github.com/kong/go-kong/kong"
	"github.com/kong/go-kong/kong/custom"
	"github.com/ssgelm/cookiejarparser"
)

var clientTimeout time.Duration

// KongRawState contains all of Kong Data
type KongRawState struct {
	Services []*kong.Service
	Routes   []*kong.Route

	Plugins []*kong.Plugin

	Upstreams []*kong.Upstream
	Targets   []*kong.Target

	Certificates   []*kong.Certificate
	SNIs           []*kong.SNI
	CACertificates []*kong.CACertificate

	Consumers      []*kong.Consumer
	CustomEntities []*custom.Entity

	KeyAuths    []*kong.KeyAuth
	HMACAuths   []*kong.HMACAuth
	JWTAuths    []*kong.JWTAuth
	BasicAuths  []*kong.BasicAuth
	ACLGroups   []*kong.ACLGroup
	Oauth2Creds []*kong.Oauth2Credential
	MTLSAuths   []*kong.MTLSAuth

	RBACRoles               []*kong.RBACRole
	RBACEndpointPermissions []*kong.RBACEndpointPermission
}

// KonnectRawState contains all of Konnect resources.
type KonnectRawState struct {
	ServicePackages []*konnect.ServicePackage
	Documents       []*konnect.Document
}

// ErrArray holds an array of errors.
type ErrArray struct {
	Errors []error
}

// Error returns a pretty string of errors present.
func (e ErrArray) Error() string {
	if len(e.Errors) == 0 {
		return "nil"
	}
	var res string

	res = strconv.Itoa(len(e.Errors)) + " errors occurred:\n"
	for _, err := range e.Errors {
		res += fmt.Sprintf("\t%v\n", err)
	}
	return res
}

// KongClientConfig holds config details to use to talk to a Kong server.
type KongClientConfig struct {
	Address   string
	Workspace string

	TLSServerName string

	TLSCACert string

	TLSSkipVerify bool
	Debug         bool

	SkipWorkspaceCrud bool

	Headers []string

	HTTPClient *http.Client

	Timeout int

	CookieJarPath string

	TLSClientCert string

	TLSClientKey string
}

type KonnectConfig struct {
	Email    string
	Password string
	Debug    bool

	Address string

	Headers []string
}

// ForWorkspace returns a copy of KongClientConfig that produces a KongClient for the workspace specified by argument.
func (kc *KongClientConfig) ForWorkspace(name string) KongClientConfig {
	result := *kc
	result.Workspace = name
	return result
}

// GetKongClient returns a Kong client
func GetKongClient(opt KongClientConfig) (*kong.Client, error) {
	var tlsConfig tls.Config
	if opt.TLSSkipVerify {
		tlsConfig.InsecureSkipVerify = true
	}
	if opt.TLSServerName != "" {
		tlsConfig.ServerName = opt.TLSServerName
	}

	if opt.TLSCACert != "" {
		certPool := x509.NewCertPool()
		ok := certPool.AppendCertsFromPEM([]byte(opt.TLSCACert))
		if !ok {
			return nil, fmt.Errorf("failed to load TLSCACert")
		}
		tlsConfig.RootCAs = certPool
	}

	if opt.TLSClientCert != "" && opt.TLSClientKey != "" {
		// Read the key pair to create certificate
		cert, err := tls.X509KeyPair([]byte(opt.TLSClientCert), []byte(opt.TLSClientKey))
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	clientTimeout = time.Duration(opt.Timeout) * time.Second
	c := opt.HTTPClient
	if c == nil {
		c = HTTPClient()
	}
	defaultTransport := http.DefaultTransport.(*http.Transport)
	defaultTransport.TLSClientConfig = &tlsConfig
	c.Transport = defaultTransport
	address := CleanAddress(opt.Address)

	headers, err := parseHeaders(opt.Headers)
	if err != nil {
		return nil, fmt.Errorf("parsing headers: %w", err)
	}
	c = kong.HTTPClientWithHeaders(c, headers)

	url, err := url.ParseRequestURI(address)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kong address: %w", err)
	}
	if opt.Workspace != "" {
		url.Path = path.Join(url.Path, opt.Workspace)
	}
	// Add Session Cookie support if required
	if opt.CookieJarPath != "" {
		jar, err := cookiejarparser.LoadCookieJarFile(opt.CookieJarPath)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize cookie-jar: %w", err)
		}
		c.Jar = jar
	}

	kongClient, err := kong.NewClient(kong.String(url.String()), c)
	if err != nil {
		return nil, fmt.Errorf("creating client for Kong's Admin API: %w", err)
	}
	if opt.Debug {
		kongClient.SetDebugMode(true)
		kongClient.SetLogger(os.Stderr)
	}
	return kongClient, nil
}

func parseHeaders(headers []string) (http.Header, error) {
	res := http.Header{}
	const splitLen = 2
	for _, keyValue := range headers {
		split := strings.SplitN(keyValue, ":", 2)
		if len(split) >= splitLen {
			res.Add(split[0], split[1])
		} else {
			return nil, fmt.Errorf("splitting header key-value '%s'", keyValue)
		}
	}
	return res, nil
}

func GetKonnectClient(httpClient *http.Client, config KonnectConfig) (*konnect.Client,
	error) {
	address := CleanAddress(config.Address)

	if httpClient == nil {
		defaultTransport := http.DefaultTransport.(*http.Transport)
		httpClient = http.DefaultClient
		httpClient.Transport = defaultTransport
	}
	headers, err := parseHeaders(config.Headers)
	if err != nil {
		return nil, fmt.Errorf("parsing headers: %w", err)
	}
	httpClient = kong.HTTPClientWithHeaders(httpClient, headers)
	client, err := konnect.NewClient(httpClient, konnect.ClientOpts{
		BaseURL: address,
	})
	if err != nil {
		return nil, err
	}
	if config.Debug {
		client.SetDebugMode(true)
		client.SetLogger(os.Stderr)
	}
	return client, nil
}

// CleanAddress removes trailling / from a URL.
func CleanAddress(address string) string {
	re := regexp.MustCompile("[/]+$")
	return re.ReplaceAllString(address, "")
}

// HTTPClient returns a new Go stdlib's net/http.Client with
// sane default timeouts.
func HTTPClient() *http.Client {
	return &http.Client{
		Timeout: clientTimeout,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: clientTimeout,
			}).DialContext,
			TLSHandshakeTimeout: clientTimeout,
		},
	}
}
