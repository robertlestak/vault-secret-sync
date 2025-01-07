package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/GoKillers/libsodium-go/cryptobox"
	"github.com/robertlestak/vault-secret-sync/pkg/driver"
	log "github.com/sirupsen/logrus"

	"github.com/google/go-github/v62/github"
	"github.com/jferrl/go-githubauth"
	"golang.org/x/oauth2"
	"golang.org/x/time/rate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type GitHubClient struct {
	Owner string `yaml:"owner,omitempty" json:"owner,omitempty"`
	Repo  string `yaml:"repo,omitempty" json:"repo,omitempty"`
	Env   string `yaml:"env,omitempty" json:"env,omitempty"`
	Org   bool   `yaml:"org,omitempty" json:"org,omitempty"`
	Merge *bool  `yaml:"merge,omitempty" json:"merge,omitempty"`

	InstallId        int    `yaml:"installId,omitempty" json:"installId,omitempty"`
	AppId            int    `yaml:"appId,omitempty" json:"appId,omitempty"`
	PrivateKeyPath   string `yaml:"privateKeyPath,omitempty" json:"privateKeyPath,omitempty"`
	PrivateKeyString string `yaml:"privateKey,omitempty" json:"privateKey,omitempty"`

	client *github.Client `yaml:"-" json:"-"`
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitHubClient) DeepCopyInto(out *GitHubClient) {
	*out = *in
	if in.Merge != nil {
		in, out := &in.Merge, &out.Merge
		*out = new(bool)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitHubClient.
func (in *GitHubClient) DeepCopy() *GitHubClient {
	if in == nil {
		return nil
	}
	out := new(GitHubClient)
	in.DeepCopyInto(out)
	return out
}

func (c *GitHubClient) Validate() error {
	l := log.WithFields(log.Fields{
		"action": "Validate",
	})
	l.Trace("start")
	if c.Owner == "" {
		return errors.New("owner is required")
	}
	// if both repo and org true, return error
	if c.Repo != "" && c.Org {
		return errors.New("either repo or org can be defined, not both")
	}
	if c.Repo == "" && c.Env != "" {
		return errors.New("repo is required for env-scoped secrets")
	}
	if c.Repo == "" && !c.Org {
		return errors.New("either repo or org is required")
	}
	return nil
}

func NewClient(cfg *GitHubClient) (*GitHubClient, error) {
	l := log.WithFields(log.Fields{
		"action": "NewClient",
	})
	l.Trace("start")
	vc := &GitHubClient{}
	jd, err := json.Marshal(cfg)
	if err != nil {
		l.Debugf("error: %v", err)
		return nil, err
	}
	err = json.Unmarshal(jd, &vc)
	if err != nil {
		l.Debugf("error: %v", err)
		return nil, err
	}
	l.Debugf("client=%+v", vc)
	l.Trace("end")
	return vc, nil
}

type rateLimitedTransport struct {
	base    http.RoundTripper
	limiter *rate.Limiter
}

func (t *rateLimitedTransport) calculateRetryDelay(resp *http.Response) time.Duration {
	l := log.WithFields(log.Fields{
		"status_code":    resp.StatusCode,
		"rate_remaining": resp.Header.Get("X-RateLimit-Remaining"),
		"rate_reset":     resp.Header.Get("X-RateLimit-Reset"),
		"retry_after":    resp.Header.Get("Retry-After"),
	})

	// Check for Retry-After header first (primarily for secondary rate limits)
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil {
			// Add buffer to the suggested retry time
			delay := time.Duration(seconds) * time.Second
			l.Infof("Using Retry-After header with delay: %v", delay)
			return delay + (5 * time.Second)
		}
	}

	// Handle secondary rate limit (abuse detection)
	if resp.StatusCode == 403 {
		delay := 120 * time.Second // More conservative 2-minute delay for 403s
		l.Infof("Detected abuse detection (403), using delay: %v", delay)
		return delay
	}

	// Handle primary rate limit
	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining == "0" {
		if resetStr := resp.Header.Get("X-RateLimit-Reset"); resetStr != "" {
			if resetTime, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
				delay := time.Until(time.Unix(resetTime, 0))
				if delay > 0 {
					// Add a 30-second buffer to ensure the rate limit has fully reset
					delay += 30 * time.Second
					l.Infof("Rate limit exhausted, waiting until reset plus buffer: %v", delay)
					return delay
				}
			}
		}
	}

	// For any rate limit related response without explicit timing information
	if resp.StatusCode == http.StatusTooManyRequests {
		delay := 60 * time.Second // Conservative 1-minute delay
		l.Infof("Received 429, using default delay: %v", delay)
		return delay
	}

	// Default exponential backoff for other cases (like 500s)
	delay := 30 * time.Second
	l.Infof("Using default delay for status %d: %v", resp.StatusCode, delay)
	return delay
}

func (t *rateLimitedTransport) shouldRetry(resp *http.Response) bool {
	// Expanded retry conditions
	switch resp.StatusCode {
	case http.StatusTooManyRequests, // 429
		http.StatusForbidden,          // 403
		http.StatusBadGateway,         // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return true
	}

	// Also retry if we're running low on remaining rate limit
	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
		if rem, err := strconv.Atoi(remaining); err == nil && rem < 10 {
			return true // Preemptively retry if we're getting close to the limit
		}
	}

	return false
}

func (t *rateLimitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	l := log.WithFields(log.Fields{
		"method": req.Method,
		"url":    req.URL.String(),
	})

	var resp *http.Response
	var err error
	retryCount := 0
	maxRetries := 100000 // Effectively unlimited retries
	baseDelay := 10 * time.Second

	for retryCount < maxRetries {
		// Always respect the rate limiter
		if err = t.limiter.Wait(req.Context()); err != nil {
			return nil, fmt.Errorf("rate limiter wait: %w", err)
		}

		// Clone the request to ensure it can be retried
		reqClone := req.Clone(req.Context())
		if req.Body != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("failed to get request body: %w", err)
			}
			reqClone.Body = body
		}

		// Make the request
		resp, err = t.base.RoundTrip(reqClone)
		if err != nil {
			// Handle non-HTTP errors (like network issues)
			delay := baseDelay * time.Duration(1<<uint(retryCount))
			l.Warnf("Transport error: %v, retrying in %v", err, delay)
			time.Sleep(delay)
			retryCount++
			continue
		}

		// Check if we need to retry
		if !t.shouldRetry(resp) {
			return resp, nil
		}

		// Calculate and apply backoff
		delay := t.calculateRetryDelay(resp)

		l.WithFields(log.Fields{
			"retry_count": retryCount + 1,
			"delay":       delay.String(),
			"status":      resp.StatusCode,
		}).Info("Retrying request due to rate limit or server error")

		resp.Body.Close()

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(delay):
			retryCount++
			continue
		}
	}

	return nil, fmt.Errorf("max retries exceeded for request: %s", req.URL)
}

func (g *GitHubClient) CreateClient(ctx context.Context) error {
	l := log.WithFields(log.Fields{
		"action": "CreateClient",
	})
	l.Trace("start")
	if g.PrivateKeyString == "" && g.PrivateKeyPath == "" {
		return errors.New("privateKey or privateKeyPath is required")
	}

	var privateKey []byte
	if g.PrivateKeyString != "" {
		privateKey = []byte(g.PrivateKeyString)
	} else {
		var err error
		privateKey, err = os.ReadFile(g.PrivateKeyPath)
		if err != nil {
			return err
		}
	}

	appTokenSource, err := githubauth.NewApplicationTokenSource(int64(g.AppId), privateKey)
	if err != nil {
		l.Error(err)
		return err
	}
	installationTokenSource := githubauth.NewInstallationTokenSource(int64(g.InstallId), appTokenSource)

	limiter := rate.NewLimiter(rate.Every(2*time.Second), 1)

	rateLimitedTransport := &rateLimitedTransport{
		base:    http.DefaultTransport,
		limiter: limiter,
	}

	httpClient := &http.Client{
		Transport: &oauth2.Transport{
			Base:   rateLimitedTransport,
			Source: installationTokenSource,
		},
	}

	g.client = github.NewClient(httpClient)
	l.Trace("end")
	return nil
}

func (g *GitHubClient) RepoID(ctx context.Context) (int64, error) {
	r, _, err := g.client.Repositories.Get(ctx, g.Owner, g.Repo)
	if err != nil {
		return 0, err
	}
	return r.GetID(), nil
}

func (vc *GitHubClient) Meta() map[string]any {
	md := make(map[string]any)
	jd, err := json.Marshal(vc)
	if err != nil {
		return md
	}
	err = json.Unmarshal(jd, &md)
	if err != nil {
		return md
	}
	return md
}

func (g *GitHubClient) Init(ctx context.Context) error {
	if err := g.CreateClient(ctx); err != nil {
		return err
	}
	if err := g.Validate(); err != nil {
		return err
	}
	return nil
}
func (g *GitHubClient) Driver() driver.DriverName {
	return driver.DriverNameGitHub
}
func (g *GitHubClient) GetPath() string {
	if g.Repo != "" {
		return g.Repo
	} else {
		return g.Owner

	}
}

func (g *GitHubClient) GetSecret(ctx context.Context, p string) ([]byte, error) {
	return nil, errors.New("not implemented")
}
func (g *GitHubClient) WriteSecret(ctx context.Context, meta metav1.ObjectMeta, path string, bSecrets []byte) ([]byte, error) {
	l := log.WithFields(log.Fields{
		"action": "WriteSecret",
		"path":   path,
		"driver": g.Driver(),
	})
	l.Trace("start")
	defer l.Trace("end")
	if g.Merge != nil && !*g.Merge {
		// first, clear out the existing secrets
		g.DeleteSecret(ctx, "")
	}
	secrets := make(map[string]interface{})
	if err := json.Unmarshal(bSecrets, &secrets); err != nil {
		return nil, err
	}
	writeErrs := make(map[string]error)
	// create secret(s) in repo for each key/value pair
	for k, v := range secrets {
		// skip values that are empty since we can't write them
		if v == "" {
			l.Debugf("skipping empty secret: %s", k)
			continue
		}
		esecret, err := g.EncryptSecret(ctx, k, v.(string))
		if err != nil {
			writeErrs[k] = err
			continue
		}
		if g.Org {
			esecret.Visibility = "all"
			_, err = g.client.Actions.CreateOrUpdateOrgSecret(ctx, g.Owner, esecret)
			if err != nil {
				writeErrs[k] = err
				continue
			}
		} else if g.Env != "" {
			rid, err := g.RepoID(ctx)
			if err != nil {
				writeErrs[k] = err
				continue
			}
			_, err = g.client.Actions.CreateOrUpdateEnvSecret(ctx, int(rid), g.Env, esecret)
			if err != nil {
				// if the error contains "404 Not Found", then the environment does not exist
				if strings.Contains(err.Error(), "404 Not Found") {
					writeErrs[k] = fmt.Errorf("environment %s does not exist", g.Env)
				} else {
					writeErrs[k] = err
				}
				continue
			}
		} else {
			_, err = g.client.Actions.CreateOrUpdateRepoSecret(ctx, g.Owner, g.Repo, esecret)
			if err != nil {
				// if the error contains "404 Not Found", then the repo does not exist
				if strings.Contains(err.Error(), "404 Not Found") {
					writeErrs[k] = fmt.Errorf("repo %s does not exist", g.Repo)
				} else {
					writeErrs[k] = err
				}
				continue
			}
		}
	}
	if len(writeErrs) > 0 {
		return nil, fmt.Errorf("error writing secrets: %v", writeErrs)
	}
	return nil, nil
}

func (g *GitHubClient) DeleteSecret(ctx context.Context, secret string) error {
	l := log.WithFields(log.Fields{
		"action": "DeleteSecret",
		"path":   secret,
		"driver": g.Driver(),
	})
	l.Trace("start")
	defer l.Trace("end")
	// delete repo secret
	secretList, err := g.ListSecrets(ctx, "")
	if err != nil {
		return err
	}
	for _, s := range secretList {
		if g.Org {
			if _, err := g.client.Actions.DeleteOrgSecret(ctx, g.Owner, s); err != nil {
				return err
			}
		} else if g.Env != "" {
			rid, err := g.RepoID(ctx)
			if err != nil {
				return err
			}
			if _, err := g.client.Actions.DeleteEnvSecret(ctx, int(rid), g.Env, s); err != nil {
				if strings.Contains(err.Error(), "404 Not Found") {
					return fmt.Errorf("environment %s does not exist", g.Env)
				} else {
					return err
				}
			}
		} else {
			if _, err := g.client.Actions.DeleteRepoSecret(ctx, g.Owner, g.Repo, s); err != nil {
				if strings.Contains(err.Error(), "404 Not Found") {
					return fmt.Errorf("repo %s does not exist", g.Repo)
				} else {
					return err
				}
			}
		}
	}
	return nil
}
func (g *GitHubClient) ListSecrets(ctx context.Context, p string) ([]string, error) {
	l := log.WithFields(log.Fields{
		"action": "ListSecrets",
		"driver": g.Driver(),
	})
	l.Trace("start")
	defer l.Trace("end")
	// list repo secrets
	var secretsList []string
	opt := &github.ListOptions{}
	for {
		var secrets *github.Secrets
		var err error
		var resp *github.Response
		if g.Org {
			secrets, resp, err = g.client.Actions.ListOrgSecrets(ctx, g.Owner, opt)
			if err != nil {
				return nil, err
			}
		} else if g.Env != "" {
			rid, err := g.RepoID(ctx)
			if err != nil {
				return nil, err
			}
			secrets, resp, err = g.client.Actions.ListEnvSecrets(ctx, int(rid), g.Env, opt)
			if err != nil {
				return nil, err
			}
		} else {
			secrets, resp, err = g.client.Actions.ListRepoSecrets(ctx, g.Owner, g.Repo, opt)
			if err != nil {
				return nil, err
			}
		}
		for _, s := range secrets.Secrets {
			secretsList = append(secretsList, s.Name)
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return secretsList, nil
}

func (g *GitHubClient) GetOrgPublicKey(ctx context.Context) (*github.PublicKey, error) {
	// get org public key
	k, _, err := g.client.Actions.GetOrgPublicKey(ctx, g.Owner)
	if err != nil {
		return nil, fmt.Errorf("error getting org public key %s: %w", g.Owner, err)
	}
	return k, nil
}

func (g *GitHubClient) GetEnvPublicKey(ctx context.Context) (*github.PublicKey, error) {
	// get env public key
	rid, err := g.RepoID(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting repo ID %s: %w", g.Repo, err)
	}
	k, _, err := g.client.Actions.GetEnvPublicKey(ctx, int(rid), g.Env)
	if err != nil {
		return nil, fmt.Errorf("error getting env public key %s %s: %w", g.Repo, g.Env, err)
	}
	return k, nil
}

func (g *GitHubClient) GetRepoPublicKey(ctx context.Context) (*github.PublicKey, error) {
	// get repo public key
	k, _, err := g.client.Actions.GetRepoPublicKey(ctx, g.Owner, g.Repo)
	if err != nil {
		return nil, fmt.Errorf("error getting repo public key %s %s: %w", g.Owner, g.Repo, err)
	}
	return k, nil
}

func (g *GitHubClient) EncryptSecret(ctx context.Context, name, plainText string) (*github.EncryptedSecret, error) {
	es := &github.EncryptedSecret{
		Name: name,
	}
	var pubKey *github.PublicKey
	var err error
	if g.Org {
		pubKey, err = g.GetOrgPublicKey(ctx)
	} else if g.Env != "" {
		pubKey, err = g.GetEnvPublicKey(ctx)
	} else {
		pubKey, err = g.GetRepoPublicKey(ctx)
	}
	if err != nil {
		return nil, err
	}
	es.KeyID = pubKey.GetKeyID()
	if es.KeyID == "" {
		return nil, errors.New("public key ID is empty")
	}
	if plainText == "" {
		return nil, errors.New("plainText is empty")
	}
	keyDec, err := base64.StdEncoding.DecodeString(pubKey.GetKey())
	if err != nil {
		return nil, err
	}
	d, serr := cryptobox.CryptoBoxSeal([]byte(plainText), []byte(keyDec))
	if serr != 0 {
		return nil, fmt.Errorf("error encrypting secret: %d", serr)
	}
	es.EncryptedValue = base64.StdEncoding.EncodeToString(d)
	return es, nil
}

func (c *GitHubClient) Close() error {
	c.client = nil
	return nil
}

func (c *GitHubClient) SetDefaults(cfg any) error {
	jd, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	nc := &GitHubClient{}
	err = json.Unmarshal(jd, &nc)
	if err != nil {
		return err
	}
	if !c.Org && nc.Org {
		c.Org = nc.Org
	}
	if c.Owner == "" && nc.Owner != "" {
		c.Owner = nc.Owner
	}
	if c.Repo == "" && nc.Repo != "" {
		c.Repo = nc.Repo
	}
	if c.Env == "" && nc.Env != "" {
		c.Env = nc.Env
	}
	if c.AppId == 0 && nc.AppId != 0 {
		c.AppId = nc.AppId
	}
	if c.InstallId == 0 && nc.InstallId != 0 {
		c.InstallId = nc.InstallId
	}
	if c.PrivateKeyPath == "" && nc.PrivateKeyPath != "" {
		c.PrivateKeyPath = nc.PrivateKeyPath
	}
	if c.PrivateKeyString == "" && nc.PrivateKeyString != "" {
		c.PrivateKeyString = nc.PrivateKeyString
	}
	// default to merge - do not delete existing secrets
	// just put ours on top
	// however if merge is explicitly set to false, then
	// we will delete all existing secrets before writing
	if c.Merge == nil || *c.Merge {
		c.Merge = nc.Merge
	} else {
		f := false
		c.Merge = &f
	}
	return nil
}
