# Deployment

This guide outlines the requirements and different deployment models for the Vault Secrets Sync service.

## Getting Started

The service can be configured with either a `YAML` or `JSON` configuration file, or via environment variables. The configuration is unmarshalled into the [`ConfigFile`](../internal/config/config.go#L62) struct. An example of a fully configured `YAML` file can be found in [examples/config-full.yaml](../examples/config-full.yaml). To configure via environment variables, you must prefix the environment variable with `VSS_` and use `_` to denote nested fields. For example, to set the `queue.type` field, you would set the environment variable `VSS_QUEUE_TYPE`.

While various examples are provided, the operator intentionally does not ship with a "default" configuration file. This is to ensure that you are aware of the security implications of each component you are enabling. This guide will walk you through each section, what it does, and how to configure it.

### `queue` Configuration

The service requires a queue for communication between the `Event Server` and the `Sync Operator`. When deployed as a single binary, the `memory` queue will be used by default. This simply uses the operator's internal memory for queuing and processing. For small to moderately sized deployments this may be sufficient, however in more production grade deployments - and when deployed in microservices mode - an external queue must be configured. In all queues, both the event server and sync operator must be able to connect to the queue, and data will only ever flow in one direction (from the outside in, never from the inside out).

Currently, the following queues are supported:

- `memory`: The default queue, which uses the operator's internal memory for queuing and processing. This is only recommended for small to moderately sized deployments, and cannot be used in a microservices deployment model.
- `redis`: The Redis queue uses a Redis instance for queuing and processing.
- `nats`: The NATS queue uses a NATS instance for queuing and processing.
- `sqs`: The SQS queue uses an AWS SQS queue for queuing and processing.

An example of a fully configured `YAML` file can be found in [examples/config-full.yaml](../examples/config-full.yaml). Here's an example of a minimal configuration file:

```yaml
queue:
  type: redis
  params:
    host: "redis"
    port: 6379
    password: ""
    db: 0
    tls:
      ca: /etc/certs/ca.crt
      cert: /etc/certs/client.crt
      key: /etc/certs/client.key
```

Fields not required in your environment can be omitted. For example, if you are not using TLS, you can omit the `tls` field. If you are not using a password, you can omit the `password` field.

### `operator` Configuration

The operator is responsible for reconciling the `VaultSecretSync` CRD and handling sync operations. Here's an example of a minimal configuration file:

```yaml
operator:
  enabled: true
  workerPoolSize: 10
  numSubscriptions: 10
```


The `workerPoolSize` field is the number of workers that will be spawned to process the events from the queue. The `numSubscriptions` field is the number of subscriptions that will be created to the queue. The number of subscriptions should be equal to or greater than the number of workers. The `workerPoolSize` field should be set to a value that is appropriate for your environment. The default value is `10`.

### `event` Configuration

The event server is responsible for listening for audit log events from Vault. The event server is required for the service to operate. It must be accessible by the respective vault instance audit log shippers, and must be able to communicate with the queue. Here's an example of a minimal configuration file:

```yaml
event:
  enabled: true
  port: 8080
  security:
    enabled: true
    tls:
      cert: /etc/certs/server.crt
      key: /etc/certs/server.key
      ca: /etc/certs/ca.crt
      clientAuth: require
```

Note that in the examples above, each service has an `enabled` field, this is where you can enable / disable particular components of the service. By default, all components are disabled. However you are still able to re-use a single configuration file across various microservice components by passing the corresponding CLI flag to enable the component rather than modifying the configuration file.

### `metrics` Configuration

The service exposes a metrics endpoint on a dedicated service metrics port (separate from the kubernetes metrics port exposed when running in kubernetes operator mode) to expose Prometheus metrics. Here's an example of a minimal configuration file:

```yaml
metrics:
  port: 8082
```

Note that the metrics endpoint also supports `security.tls` configuration, it has simply been omitted from the example for brevity. The metrics server also exposes a `/healthz` endpoint that can be used to check the health of the service and its dependencies.

### `stores` Configuration

Each `VaultSecretSync` configuration is entirely self-contained - the `spec` contains all the fields necessary to perform the sync. However you can use the `stores` configuration to set defaults for all `VaultSecretSync` resources. This is useful if you have multiple `VaultSecretSync` resources that share the same configuration. Here's an example of a minimal configuration file:

```yaml
stores:
  vault:
    address: "https://vault.example.com"
    
  github:
    owner: "example-org"
```

Fields explictly defined in the `VaultSecretSync` resource will take precedence over the defaults set in the `stores` configuration, however if a field is not provided in the `VaultSecretSync` resource, the default value from the `stores` configuration will be used. Defaults are evaluated at runtime and are not persisted back to the backend. This can be both a feature and a bug, depending on your use case. Once you set a central default, be cognizant of the fact that changing the default _will_ change the behavior of existing `VaultSecretSync` resources. For this reason it's generally recommended to not set global defaults and instead rely on fields being explicitly declared on the `VaultSecretSync` resources themselves, unless you know for certain that you want to change the behavior of all resources at once.

With this said, the fields defined will only work if the operator has the proper access to the stores. For more details on how to configure the operator to access the stores, see the [Security](./SECURITY.md) documentation.

## Deploying in Kubernetes

The service can be deployed in Kubernetes using the provided Helm chart. The Helm chart is located in the `deploy/charts` directory of the repository. The chart is designed to be as flexible as possible, and allows you to configure the service using a `values.yaml` file. The chart is designed to be deployed in a microservices architecture, where the webhook service is deployed in one container and the sync operator is deployed in another, however it does support a monolithic deployment as well. The chart also deploys a CRD to enable configuration of the sync service through native Kubernetes resources.

Once you have your `values.yaml` file configured, you can deploy the service using the following command:

```shell
helm install -n vault-secret-sync --create-namespace \
  vault-secret-sync ./deploy/charts/vault-secret-sync \
  -f /path/to/values.yaml
```

Note this will install the `VaultSecretSync` CRD by default. While recommended to use the CRD when deploying in Kubernetes it is _technically_ not required, and so if you do not want to install the CRDs with the rest of the chart, you can pass the `--skip-crds` flag to the `helm install` command.

The chart will deploy one `Service` object for the event service with a `ClusterIP` type. This must be made accessible from the Vault audit log shipper(s) in whatever manner suits your environment.

You can mount volumes, secrets, and env vars to any of the components through the values file, so if you need to mount a secret to the event server, you can do so through the `values.yaml` file.

If you're not a fan of using Helm to manage your resources, you can always replace `helm install` with `helm template` and pipe the output to `kubectl apply -f -` to apply the resources directly to your cluster.

```shell
helm template -n vault-secret-sync \
  vault-secret-sync ./deploy/charts/vault-secret-sync \
  -f /path/to/values.yaml | kubectl apply -f -
```

## Shipping Logs

This service relies on the audit logs as shipped by HashiCorp Vault. You must have an [audit device](https://developer.hashicorp.com/vault/docs/audit) configured in your Vault instance to ship logs to the service. You must configure the webhook endpoint in your audit device to point to the `/events` endpoint of the service. It is recommended to include the `X-Vault-Tenant` header in the request to the service to identify the source of the event. This is especially important if you are syncing secrets from multiple Vault instances. This is discussed more in [Usage - Source Determination](./USAGE.md#source-determination). Below is a sample Fluentd configuration that ships logs to the service. If you have event server token-based security enabled, you will also need to include the `X-Vault-Secret-Sync-Token` header in the request. While your security posture may vary, it's generally recommended to use multiple layers of security, such as internal networking, IP whitelisting, service mesh RBAC, and token-based security.

### Fluentd Configuration Example

**Token based auth**

```xml
<store>
  @type http
  endpoint https://vault-secret-sync/events
  headers {"x-vault-tenant": "https://vault.example.com", "x-vault-secret-sync-token": "99CFF209-9E67-4B22-880F-E15DAC3C1CEE"}
  open_timeout 2
  <format>
    @type json
  </format>
  <buffer>
    flush_interval 10s
  </buffer>
</store>
```

**TLS Client Cert Auth**

```xml
<store>
  @type http
  endpoint https://vault-secret-sync/events
  headers {"x-vault-tenant": "https://vault.example.com"}
  tls_ca_cert_path /path/to/ca.crt
  tls_client_cert_path /path/to/client.crt
  tls_private_key_path /path/to/client.key
  open_timeout 2
  <format>
    @type json
  </format>
  <buffer>
    flush_interval 10s
  </buffer>


  Once your fluentd is up and running, configure your vault audit device to ship logs to the fluentd endpoint.


```bash
vault audit enable socket address=fluentd:24224 socket_type=tcp
```