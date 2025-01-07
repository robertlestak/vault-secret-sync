# Usage

## YAML Configuration

```yaml
apiVersion: vaultsecretsync.lestak.sh/v1alpha1
kind: VaultSecretSync
metadata:
  name: "example-sync"
  namespace: "default"
spec:
  dryRun: false
  syncDelete: false
  suspend: false
  source:
    address: "https://vault.example.com"
    path: "foo/bar/(.*)"
    namespace: "robertlestak/example"
  filters:
    regex:
      include:
      - "foo/bar/hello-[0-9]+"
      exclude:
      - "foo/bar/no[^abc]+"
    paths:
      include:
      - "foo/bar/hello"
      exclude:
      - "foo/bar/no"
  dest:
  - vault:
      address: "https://vault2.example.com"
      path: "hello/world/$1"
      namespace: "robertlestak/example"
  - vault:
      address: "https://vault3.example.com"
      path: "another/vault"
  - aws:
      name: "example-secret"
      region: "us-west-2"
      roleArn: "arn:aws:iam::123456789012:role/role-name"
      encryptionKey: "alias/aws/secretsmanager"
      replicaRegions: ["us-east-1"]
  - github:
      repo: "example-repo"
      owner: "robertlestak"
  - gcp:
      project: "example-project"
      name: "example-secret"
  - http:
      url: "https://example.com/my/app"
      method: "POST"
      headerSecret: "default/header-secret"
      headers:
        Content-Type: "application/json"
      template: |
        {
          "custom": {
            "{{ .Key }}": "{{ .Value }}"
          }
        }
  webhooks:
  - event: success
    url: "https://example.com/success"
    method: "POST"
    headers:
      Content-Type: "application/json"
    template: |
      {
        "custom": {
          "event": "{{ .Event }}",
          "message": "{{ .Message }}",
          "name": "{{ .VaultSecretSync.Name }}"
          "source.address": "{{ .VaultSecretSync.Spec.Source.Address }}"
        }
      }
  - event: failure
    url: "https://example.com/failure"
    method: "POST"
    headers:
      Content-Type: "application/json"
    template: |
      {
        "custom": {
          "event": "{{ .Event }}",
          "message": "{{ .Message }}",
          "name": "{{ .VaultSecretSync.Name }}"
          "source.address": "{{ .VaultSecretSync.Spec.Source.Address }}"
        }
      }
```

`metadata` is that can be used to identify the sync operation. Both can be ommitted, however if `name` is omitted, to trigger a manual sync you will first need to authenticate and access the `/configs` endpoint to get the auto-generated name for your respective config.

`spec.source` and `spec.dest` are the source and destination of the sync operation.

`authMethod` and `role` can be omitted to use the default global method and role, if defined. If they are specified, they will be used to authenticate to the Vault instance. Optionally, `token` can be provided as a mustache template referencing an environment variable. If provided, this will always override configured auth methods.

`path` can either be provided as a path to a single vault kv secret, or as a regex string. If regex, all matching child secrets will be synced. Wildcarding will recurse into child paths, so `foo/test/(.*)` will sync `foo/test/foo` and `foo/test/foo/bar`, but not `foo/test2/foo`. However `foo/test(.*)` will sync `foo/test` and `foo/test2`. You can also use capture groups to rewrite the path in the destination. For example, `foo/(test)/(bar)` will sync `foo/test/bar` and rewrite it to `test/bar` in the destination.

If you set `source.cidr` to the CIDR in which the source vault is deployed (as seen from ingestion point - so if this is a public Vault, this will be the outbound NAT/IGW), it will enable multiple source vaults to sync through a single instance of the operator. You can also set `x-vault-tenant` header in the log shipping config to specify the source vault from which that log is coming from.

### Source Determination

By default, the Vault Audit log contains no contextual information about what Vault it is coming from. To enable this operator to connect to multiple vaults, multi-tenant source determination logic is used based on the following order of precendence.

- `X-Vault-Tenant` Header: If an `x-vault-tenant` header is set to a Vault URL (as defined in the example fluentd config below), this will be used to perform an exact lookup against configured Vault Source Addresses.
- `X-Forwarded-For` Header: If an `x-forwarded-for` header is set (and the `x-vault-tenant` is not), the operator will perform a source lookup by finding the `source.cidr` which contains the caller IP.
- `Remote IP Address`: If both the `x-vault-tenant` and `x-forwarded-for` headers do not exist, the operator will use the caller's IP address and will perform a source lookup by finding the `source.cidr` which contains the caller IP.


### Filters

Filters can be applied to the sync to include or exclude secrets based on either a regex pattern or a path pattern. The path filter is an explicit match, while the regex filter is a regex pattern match. If both filters are present, the secret must match both filters to be included in the sync.

```yaml
  filters:
    regex:
      include:
      - "foo/bar/hello-[0-9]+"
      exclude:
      - "foo/bar/no[^abc]+"
    paths:
      include:
      - "foo/bar/hello"
      exclude:
      - "foo/bar/no"
```


### Destination Configuration

The destination is configured in the same way as the source, with the exception that the `driver` field can be specified. If no driver is specified, the default driver is `vault`.

#### Vault (Driver: `vault`)

The Vault destination driver will write the secret to the target Vault instance.

```yaml
  dest:
  - vault:
      address: "https://vault.example.com"
      path: "foo/test2/(.*)"
      namespace: ""
      authMethod: ""
      role: ""
      ttl: 1m # optional, defaults to token default lease time
      merge: false # optional, default false. false will overwrite existing secrets with values from vault, merge will merge the two, overwriting only the keys that are present in the new secret
```

#### GitHub (Driver: `github`)

The GitHub destination driver will write the secret to a GitHub repository or organization.

```yaml
  dest:
  - github:
      repo: "example-repo"
      env: "" # optional, default empty. Set to a specific environment to sync to within a repo if needed
      owner: "robertlestak" # optional, will default to the company org
      org: false # optional, default false. set to true to set org secret rather than repo secret
      merge: false # optional, default true. false will overwrite existing secrets with values from vault, merge will merge the two
```

Note that since GitHub secrets do not have a concept of pathing, if you are syncing a wildcard source path, the secrets will be overwritten in the destination repository with a last-write-wins strategy.

#### AWS Secrets Manager (Driver: `aws`)

The AWS destination driver will write the secret to AWS Secrets Manager.

```yaml
  dest:
  - aws:
      name: "example-secret"
      region: "us-west-2" # optional, default us-east-1
      roleArn: "arn:aws:iam::123456789012:role/role-name" # optional, default empty. Set to a specific role to assume when writing to secrets manager
      encryptionKey: "alias/aws/secretsmanager" # optional, default empty. Set to a specific KMS key to use for encryption
      replicaRegions: [] # optional, default empty. Set to a list of regions to replicate the secret to
```

#### GCP Secret Manager (Driver: `gcp`)

The GCP destination driver will write the secret to GCP Secret Manager in the specified project.

```yaml
  dest:
  - gcp:
      project: "example-project"
      name: "example-secret"
      replicationLocations: [] # optional, default empty. Set to a list of regions to replicate the secret to. If empty, all regions will be used
```


Note that since GCP Secret Manager does not support the `/` character, the sync operator will replace `/` with `-` in the secret name. This generally only applies when using a wildcard source path.



#### HTTP (Driver: `http`)

The HTTP destination driver will make an HTTP request to the specified URL with the secret data as the body of the request. By default this will be a POST request with a JSON body, but the method, headers, and body can be customized. Note that this will be sending your secrets in plain text to the specified URL, so ensure that the destination is within your control and secure.

```yaml
  dest:
  - http:
      url: "https://example.com/my/app"
      method: "POST" # optional, default POST. Set to the HTTP method to use for the request
      headerSecret: "default/header-secret" # optional, default empty. Set to the name of a kubernetes secret in the format namespace/name to use as the header KV pairs for the request
      headers: # optional, default empty. Set to a map of headers to include in the request
        Content-Type: "application/json"
      template: | # optional, default empty. Set to a template to use for the request body. The template is a Go template with the following variables available: .Key, .Value, .Namespace, .Path, .Secret, .Timestamp
        {
          "custom": {
            "{{ .Key }}": "{{ .Value }}"
          }
        }
```

#### Webhooks

Webhooks can be configured to send a POST request to a specified URL when a sync event occurs. The event can be either `success` or `failure`, and the request will include a JSON body with information about the event. The template can be customized to include any information from the sync event.

```yaml
  webhooks:
  - event: success
    url: "https://example.com/success"
    method: "POST" # optional, default POST. Set to the HTTP method to use for the request
    headers: # optional, default empty. Set to a map of headers to include in the request
      Content-Type: "application/json"
    template: | # optional, default empty. Set to a template to use for the request body. The template is a Go template with the following variables available: .Event, .Message, .VaultSecretSync
      {
        "custom": {
          "event": "{{ .Event }}",
          "message": "{{ .Message }}",
          "name": "{{ .VaultSecretSync.Name }}"
          "source.address": "{{ .VaultSecretSync.Spec.Source.Address }}"
        }
      }
  - event: failure
    url: "https://example.com/failure"
    method: "POST" # optional, default POST. Set to the HTTP method to use for the request
    headers: # optional, default empty. Set to a map of headers to include in the request
      Content-Type: "application/json"
    template: | # optional, default empty. Set to a template to use for the request body. The template is a Go template with the following variables available: .Event, .Message, .VaultSecretSync
      {
        "custom": {
          "event": "{{ .Event }}",
          "message": "{{ .Message }}",
          "name": "{{ .VaultSecretSync.Name }}"
          "source.address": "{{ .VaultSecretSync.Spec.Source.Address }}"
        }
      }
```

## Operations

### Kubernetes

When deployed in Kubernetes, `VaultSecretSync` operations are exposed through the Kubernetes API. `VaultSecretSync` resources can be created, updated, and deleted through the Kubernetes API.

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: vaultsecretsync.lestak.sh/v1alpha1
kind: VaultSecretSync
metadata:
  name: example
  namespace: default
spec:
  source:
    address: "https://vault.example.com"
    path: "foo/bar"
  dest:
  - github:
      repo: "example-repo"
      owner: "robertlestak"
  - aws:
      name: "example-secret"
      region: "us-west-2"
EOF
```

Once created, the operator will begin syncing secrets from the source to the destination. You can trigger an immediate sync by annotating the `VaultSecretSync` resource with `force-sync`.

```bash
kubectl annotate vaultsecretsync example force-sync=$(date +%s) --overwrite
```

To sync all `VaultSecretSync` resources in a namespace, you can use the following command:

```bash
kubectl get vaultsecretsync -n example -o name | xargs -I {} kubectl annotate -n example {} force-sync=$(date +%s) --overwrite
```

To sync all `VaultSecretSync` resources in all namespaces, you can use the following command:

```bash
for ns in $(kubectl get ns -o name | cut -d/ -f2); do kubectl get vaultsecretsync -n $ns -o name | xargs -I {} kubectl annotate -n $ns {} force-sync=$(date +%s) --overwrite; done
```
