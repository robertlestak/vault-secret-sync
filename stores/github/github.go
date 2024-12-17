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
	// First check for secondary rate limit (abuse detection)
	if resp.StatusCode == 403 {
		if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil {
				return time.Duration(seconds) * time.Second
			}
		}
		// If no Retry-After header but it's a 403, use a longer default delay
		return 60 * time.Second
	}

	// Check standard rate limit headers
	remaining := resp.Header.Get("X-RateLimit-Remaining")
	if remaining == "0" {
		if resetStr := resp.Header.Get("X-RateLimit-Reset"); resetStr != "" {
			if resetTime, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
				delay := time.Until(time.Unix(resetTime, 0))
				if delay > 0 {
					// Add a small buffer to ensure the rate limit has reset
					return delay + (2 * time.Second)
				}
			}
		}
	}

	// Default exponential backoff for other cases
	return time.Second * 10
}

func (t *rateLimitedTransport) shouldRetry(resp *http.Response) bool {
	// Retry on rate limit (429), abuse detection (403), and server errors (500s)
	return resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode == http.StatusForbidden ||
		(resp.StatusCode >= 500 && resp.StatusCode <= 599)
}

func (t *rateLimitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error
	maxRetries := 10000
	retryCount := 0
	baseDelay := time.Second * 10 // Default exponential backoff base delay

	for retryCount < maxRetries {
		// Wait for the rate limiter
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
			return nil, fmt.Errorf("round trip: %w", err)
		}

		// Successful response, return
		if !t.shouldRetry(resp) {
			return resp, nil
		}

		// Log and determine the delay
		delay := t.calculateRetryDelay(resp)
		if delay == 0 {
			// Use exponential backoff if no explicit retry delay provided
			delay = baseDelay * (1 << retryCount) // Exponential backoff
		}

		log.WithFields(log.Fields{
			"status_code":    resp.StatusCode,
			"retry_count":    retryCount + 1,
			"retry_delay":    delay.String(),
			"rate_remaining": resp.Header.Get("X-RateLimit-Remaining"),
			"rate_reset":     resp.Header.Get("X-RateLimit-Reset"),
			"retry_after":    resp.Header.Get("Retry-After"),
		}).Warn("GitHub API rate limit hit, retrying request")

		// Close the response body to avoid leaking
		resp.Body.Close()

		// Wait before retrying
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(delay):
			retryCount++
			continue
		}
	}

	return resp, fmt.Errorf("max retries exceeded for request: %s", req.URL)
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

	// Create a rate limiter - reduced to 2 requests per second to be more conservative
	limiter := rate.NewLimiter(rate.Every(time.Second/2), 1)

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
		return nil, err
	}
	return k, nil
}

func (g *GitHubClient) GetEnvPublicKey(ctx context.Context) (*github.PublicKey, error) {
	// get env public key
	rid, err := g.RepoID(ctx)
	if err != nil {
		return nil, err
	}
	k, _, err := g.client.Actions.GetEnvPublicKey(ctx, int(rid), g.Env)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (g *GitHubClient) GetRepoPublicKey(ctx context.Context) (*github.PublicKey, error) {
	// get repo public key
	k, _, err := g.client.Actions.GetRepoPublicKey(ctx, g.Owner, g.Repo)
	if err != nil {
		return nil, err
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
