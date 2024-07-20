# Security

As this service is effectively an event bus for your various secret stores and therefore must be granted read and write access to those stores, understanding your security posture is critical. Throughout the documentation presented in this respository, every effort has been made to present both the "less-secure" and "production-grade" approaches, and to call out where security decisions must be made. With that said, as with any solution you adopt, it is critical to understand the security implications of the decisions you make. The solution is released as MIT licensed open source software, and as such, the authors and contributors cannot be held responsible for any security incidents that may occur as a result of using this software.

## Design Considerations

### Secure by Default

The service is designed to be secure by default, preferrring more explicit configuration as opposed to insecure defaults. This does mean initial set up will not be one-liner fire-and-forget. That is intentional, as it forces you to think about the security implications of the decisions you are making rather than starting up the process insecurely, forgetting about it, and then being surprised when something goes wrong. If started with no configuration and no command line flags, the service will exit with an error. All components of the service default to disabled, you must explicitly enable the components you wish to use.

### Segregation of Duties

By default, all components of the app default to disabled. This is to ensure that you are only enabling the components you need. To run in "single binary mode", you must explicitly enable each component. This is to ensure that you are aware of the security implications of each component you are enabling. This also enables you to more easily deploy the service in a microservices architecture, where you can run the webhook service in one container and the sync operator in another.


## Security Configuration

### API Server

The service exposes one API process, the `Event Server` which is responsible for handling audit log events from Vault.

The API server can be secured either with mTLS, an auth token string, or both. Of course you can also - and are recommended to - further secure communication via service mesh, network policies, or other network security controls.

If in doubt, it is recommended to use mTLS for all communication with the service. This will ensure that all communication is encrypted and that the client is authenticated. If you are running in a Kubernetes cluster, you can use `cert-manager` to automatically provision certificates for your services. If using token auth, the token must be passed as a `X-Vault-Secret-Sync-Token` header in the request.

### Queue

If deployed in HA / microservice mode, the service will rely on a queue to communicate between the `Event Server` and the `Sync Operator`.

The event server only needs to publish to the queue, and the sync operator only needs to consume from the queue. All appropriate measures should be taken to secure the queue, including network policies, authentication, and encryption.

### Secret Stores

The service must be granted read and write access to the secret stores it is syncing to. This is a critical security consideration, as the service will be able to read and write secrets to the destination store. In a sync operation, the service will only read from the source and write to the destination.

It is recommended to use a service account with the least privileges necessary to perform the sync operation. For example, if you are syncing to a Vault instance, you should create a policy that only allows the service to read and write to the paths it needs to sync.

All operations performed by the service include an `X-Vault-Sync: true` header to identify the action as being performed by the sync service.

As the operator itself is effectively `root` for lack of a better analogy, it is critical to ensure that the operator is only deployed in environments where it can be trusted. Furthermore, as the operator will dutifully do what it is told to do, it is critical to ensure proper RBAC and policy is in place around modifying operator and / or sync configurations. If deployed in a Kubernetes cluster, it is recommended to use the Kubernetes native RBAC system to limit access to the operator and sync configurations.

Every sync operation will instantiate a new client to the source and destination secret store, and will close the client after the operation is complete. At no time are client objects reused between sync operations. This is to ensure that the client is not left open and vulnerable to attack.

#### AWS

If you are running in AWS EKS, you can use IAM roles to grant the operator access to the AWS Secrets Manager. If you are running in a different environment, you can use the `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` environment variables to provide the operator with the necessary credentials. If you are using access keys, it is recommended to rotate these regularly, and utilize a project such as [External Secrets Operator](https://external-secrets.io/latest/) to manage the lifecycle of the access keys into the operator.

For cross-account access you must configure an IAM role in your target account which can be assumed by the identity associated with the operator.

Your role will need to have the following permissions:

- `secretsmanager:CreateSecret`
- `secretsmanager:UpdateSecret`
- `secretsmanager:PutSecretValue`
- `secretsmanager:DeleteSecret`
- `secretsmanager:ReplicateSecretToRegions`
- `secretsmanager:RemoveRegionsFromReplication`
- `secretsmanager:ListSecrets`
- `secretsmanager:ListSecretVersionIds`
- `secretsmanager:DescribeSecret`
- `secretsmanager:TagResource`

Here is an example policy document:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "secretsmanager:CreateSecret",
                "secretsmanager:UpdateSecret",
                "secretsmanager:PutSecretValue",
                "secretsmanager:DeleteSecret",
                "secretsmanager:ReplicateSecretToRegions",
                "secretsmanager:RemoveRegionsFromReplication",
                "secretsmanager:ListSecrets",
                "secretsmanager:ListSecretVersionIds",
                "secretsmanager:DescribeSecret",
                "secretsmanager:TagResource"
            ],
            "Resource": "*"
        }
    ]
}
```

and an example trusted entity configuration:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::1234567890:role/vault-secret-sync"
      },
      "Action": ["sts:AssumeRole", "sts:TagSession"],
    }
  ]
}
```

#### GCP

If you are running in GCP GKE, you can use GCP service accounts to grant the operator access to GCP Secret Manager. If you are running in a different environment, you can use the `GOOGLE_APPLICATION_CREDENTIALS` environment variable to provide the operator with the necessary credentials. If you are using service accounts, it is recommended to rotate these regularly, and utilize a project such as [External Secrets Operator](https://external-secrets.io/latest/) to manage the lifecycle of the service account keys into the operator.

This identity associated with the operator must be granted the following permissions:

- `secretmanager.versions.access`
- `secretmanager.versions.add`
- `secretmanager.versions.create`
- `secretmanager.versions.destroy`
- `secretmanager.versions.disable`
- `secretmanager.versions.enable`
- `secretmanager.versions.get`
- `secretmanager.versions.list`
- `secretmanager.versions.restore`
- `secretmanager.versions.update`
- `secretmanager.secrets.access`
- `secretmanager.secrets.addVersion`
- `secretmanager.secrets.create`
- `secretmanager.secrets.delete`
- `secretmanager.secrets.get`
- `secretmanager.secrets.list`
- `secretmanager.secrets.update`

#### GitHub

GitHub requires a GitHub App installed in the account with access to the level of secrets you desire. When you create your GitHub App, it will have an `installId`, `appId`, and `privateKey`. You will need to provide these to the operator to authenticate with GitHub. It is recommended to rotate the private key regularly, and utilize a project such as [External Secrets Operator](https://external-secrets.io/latest/) to manage the lifecycle of the private key into the operator.

Heere is an example store configuration:

```yaml
stores:
  github:
    installId: 12345
    appId: 67890
    privateKeyPath: "/path/to/private/key"
```

### HashiCorp Vault

If you are running in a Kubernetes cluster, you can use the Kubernetes auth method to authenticate the operator with Vault. If you are running in a different environment, you can use the `VAULT_TOKEN` environment variable to provide the operator with the necessary token. If you are using tokens, it is recommended to rotate these regularly, and utilize a project such as [External Secrets Operator](https://external-secrets.io/latest/) to manage the lifecycle of the tokens into the operator.


## Vulnerability Reporting

If you believe you have found a security vulnerability in this project, please report it privately to the project maintainers. If you are unsure whether the issue is a security vulnerability, please report it anyway. We take all reports seriously and will respond promptly to your inquiry. Please do not disclose the issue publicly until we have had a chance to address it. You can report a security vulnerability by emailing [robert@lestak.sh](mailto:robert@lestak.sh). Please include the word "SECURITY" in the subject line.