# HashiCorp Vault Secret Sync

vault-secret-sync provides fully automated real-time secret syncronization from HashiCorp Vault to other remote secret stores. This enables you to take advantage of natively integrated cloud secret stores while maintaining an authoratative single source of truth in Vault. Both Open Source and Enterprise versions of Vault are supported.

Currently, the following secret stores are supported:

- Vault (kv2)
- AWS Secrets Manager
- GCP Secret Manager
- GitHub Repository
- GitHub Organization

## High Level Architecture

![High Level Architecture](./docs/architecture/HLA.drawio.png)

The above reference architecture can be viewed as a "logical" architecture, as the service can be deployed in a variety of ways. For more detailed information on different deployment models, see [Deployment](./docs/DEPLOYMENT.md).

## Deployment

The operator is cloud agnostic and can be run either in a Kubernetes cluster or as a standalone service. When deployed in Kubernetes, the operator will also deploy a CRD to enable configuration of the sync service through native Kubernetes resources. When deployed as a standalone service, configuration is done through a single YAML file. See [Deployment](./docs/DEPLOYMENT.md) for more detailed information on service architecture and deployment options.

### Configuration

Configuration is done via a YAML spec that defines the source and destination of the secrets to be synced. The service will listen for audit log events from the source Vault instance and sync the secrets to the destination secret store.

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
    path: "foo/bar/hello"
    namespace: "robertlestak/example"
  filters:
    regex:
      include:
      - "foo/bar/hello-[0-9]+"
      exclude:
      - "foo/bar/no[^abc]+"
    path:
      include:
      - "foo/bar/hello"
      exclude:
      - "foo/bar/no"
  transforms:
    include:
    - "password"
    - "supports_regex_too.*"
    exclude:
    - "secret"
    - "remove_private.*"
    rename:
    - from: "old_key"
      to: "new_key"
    template: |
      {
        "new_password": "{{ .password }}",
        {{ if eq .customField "someValue" }}
        "conditional_field": "included_value"
        {{ else }}
        "conditional_field": "excluded_value"
        {{ end }}
      }
  dest:
  - vault:
      address: "https://vault2.example.com"
      path: "hello/world"
      namespace: "robertlestak/example"
  - aws:
      name: "example-secret"
      region: "us-west-2"
      roleArn: "arn:aws:iam::123456789012:role/role-name"
      encryptionKey: "alias/aws/secretsmanager"
      replicaRegions: ["us-east-1"]
      tags:
        key: "value"
        another: "tag"
  - github:
      repo: "example-repo"
      owner: "robertlestak"
  - gcp:
      project: "example-project"
      name: "example-secret"
      labels:
        key: "value"
        another: "label"
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
  notifications:
  - email:
      events: ["success", "failure"]
      to: "user@example.com"
      from: "noreply@example.com"
      subject: "VaultSecretSync Notification - {{ .Event }}"
      body: |
        The sync operation has completed with status: {{ .Event }}.
        Details:
        Name: {{ .VaultSecretSync.Name }}
        Source: {{ .VaultSecretSync.Spec.Source.Address }}
        Destination: {{ .VaultSecretSync.Spec.Dest | json }}
  - slack:
      events: ["failure"]  
      url: "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"
      body: |
        The sync operation has failed.
        Details:
        Name: {{ .VaultSecretSync.Name }}
        Source: {{ .VaultSecretSync.Spec.Source.Address }}
        Error: {{ .Message }}
  - webhook:
      events: ["success", "failure"]
      url: "https://example.com/webhook"
      method: "POST"
      headerSecret: "my-secret-in-this-namespace"
      headers:
        Content-Type: "application/json"
      body: |
        {
          "status": "{{ .Event }}",
          "name": "{{ .VaultSecretSync.Name }}",
          "source": "{{ .VaultSecretSync.Spec.Source.Address }}",
          "destination": {{ .VaultSecretSync.Spec.Dest | json }},
          "message": "{{ .Message }}"
        }
```

See [Security](./docs/SECURITY.md) for more detailed information on configuration options and identity configuration.

### Path Configuration

The `Source` path can either be provided as an exact path in Vault, i.e. `kv/hello-world/my/secret`, or it can be provided as a regex pattern to match multiple paths. You can use regex capture groups to then rewrite the paths in the destination secret store(s).

For example, if you have a source path of `kv/hello-world/my/secret-(.*)`, and a destination path of `hello/world/$1`, the secret `kv/hello-world/my/secret-1` will be synced to `hello/world/1`.

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

### Transforms

Transforms can be applied to the secret data before it is synced to the destination.

#### Exclude

Exclude specific keys from the sync. All other keys not specified will be synced.

```yaml
  transforms:
    exclude:
    - "key_to_exclude"
```

#### Include

Include only specific keys in the sync. All other keys not specified will be excluded.

```yaml
  transforms:
    include:
    - "key_to_include"
```

#### Rename

Rename a key in the secret data. This will rename the key in the secret data before it is synced to the destination.

```yaml
  transforms:
    rename:
    - from: "old_password"
      to: "renames_processed_first"
```

#### Template

Apply a Go template to the secret data. The template will be passed the secret object as a `map[string]any`, and the result of the template will be the new secret data bytes. If writing to a backend which requires key-value pairs, the template should output a JSON object which can marshal to a `map[string]any`.

```yaml
  transforms:
    template: |
      {
        "then_templates_are_processed": "{{ .renames_processed_first }}"
      }
```

```yaml
  transforms:
    template: |
      {{ .simple_string_data }}
```

### Destination Configuration

While the Secret Driver is technically a generic interface, currently, the service implements a one-way secret sync from the source to the destination, where only `vault` type data stores are supported as the source. The destination can be any of the supported secret stores. This is by design, to ensure that the source of truth is always Vault.

#### Vault (Driver: `vault`)

The Vault destination driver will write the secret to the target Vault instance.

```yaml
  dest:
  - vault:
      address: "https://vault.example.com"
      path: "foo/test2/$1"
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

Note that since GitHub secrets do not have a concept of pathing, if you are syncing a multi-level regex source path, the secrets will be overwritten in the destination repository. If you need to sync multiple source paths to a single destination repository, you will need to set `merge: true`.

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
      tags: # optional, default empty. Set to a map of tags to apply to the secret
        key: "value"
        another: "tag"
```

#### GCP Secret Manager (Driver: `gcp`)

The GCP destination driver will write the secret to GCP Secret Manager in the specified project.

```yaml
  dest:
  - gcp:
      project: "example-project"
      name: "example-secret"
      replicationLocations: [] # optional, default empty. Set to a list of regions to replicate the secret to. If empty, all regions will be used
      labels: # optional, default empty. Set to a map of labels to apply to the secret
        key: "value"
        another: "label"
```

Note that since GCP Secret Manager does not support the `/` character, the sync operator will replace `/` with `-` in the secret name. This generally only applies when using a regex source path.

#### HTTP (Driver: `http`)

The HTTP destination driver will make an HTTP request to the specified URL with the secret data as the body of the request. By default this will be a POST request with a JSON body, but the method, headers, and body can be customized. Note that this will be sending your secrets in plain text to the specified URL, so ensure that the destination is within your control and secure.

```yaml
  dest:
  - http:
      url: "https://example.com/my/app"
      method: "POST" # optional, default POST. Set to the HTTP method to use for the request
      headers: # optional, default empty. Set to a map of headers to include in the request
        Content-Type: "application/json"
      template: | # optional, default empty. Set to a template to use for the request body. The template is a Go template with the following variables available: .Key, .Value, .Namespace, .Path, .Secret, .Timestamp
        {
          "custom": {
            "{{ .Key }}": "{{ .Value }}"
          }
        }
```

#### Notifications

Notifications can be configured to send a message to a configured receiver when a sync event occurs. The event can be either `success` or `failure`, and the request will include a JSON body with information about the event. The template can be customized to include any information from the sync event.

```yaml
  notifications:
  - email:
      events: ["success", "failure"]
      to: "user@example.com"
      from: "noreply@example.com"
      subject: "VaultSecretSync Notification - {{ .Event }}"
      body: |
        The sync operation has completed with status: {{ .Event }}.
        Details:
        Name: {{ .VaultSecretSync.Name }}
        Source: {{ .VaultSecretSync.Spec.Source.Address }}
        Destination: {{ .VaultSecretSync.Spec.Dest | json }}
  - slack:
      events: ["failure"]  
      url: "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"
      urlSecret: "default/slack-url-secret" # optional, default empty. Set to the path of the secret containing the slack webhook URL
      urlSecretKey: "url" # optional, default "url". Set to the key of the secret containing the slack webhook URL when using urlSecret
      body: |
        The sync operation has failed.
        Details:
        Name: {{ .VaultSecretSync.Name }}
        Source: {{ .VaultSecretSync.Spec.Source.Address }}
        Error: {{ .Message }}
  - webhook:
      events: ["success", "failure"]
      url: "https://example.com/webhook"
      method: "POST"
      headers:
        Content-Type: "application/json"
      body: |
        {
          "status": "{{ .Event }}",
          "name": "{{ .VaultSecretSync.Name }}",
          "source": "{{ .VaultSecretSync.Spec.Source.Address }}",
          "destination": {{ .VaultSecretSync.Spec.Dest | json }},
          "message": "{{ .Message }}"
        }
```

For the `email` notification, be sure to set `notifications.email` values in your config with your SMTP server information. Remember that you can use environment variables for sensitive information, eg `VSS_NOTIFICATIONS_EMAIL_PASSWORD=foobar`.

## Dry Run

The operator can be run in dry run mode to simulate the sync operation without actually writing any secrets to the destination. This can be useful for testing the sync operation before running it in production.

```yaml
spec:
  dryRun: true
```

This will authenticate to both secret stores, read the value from the source, and log that it would have attempted to write the secret to the destination. You can also see this in the kubernetes events for the sync resource.

## Sync Delete

By default, the sync operator will sync creations, updates, _and deletions_. If you only want to sync creations and updates, you can set the `syncDelete` flag to `false`.

```yaml
spec:
  syncDelete: false
```

This will prevent the operator from deleting secrets in the destination secret store when they are deleted in the source. Note that with this flag set, there may be a divergence between the source and destination secret stores if secrets are deleted in the source but that is not reflected in the destination. However this may be necessary if you have a many-to-one configuration where multiple source paths are synced to a single destination path, and you do not want to delete the entire destination path when a single source path is deleted/recreated.