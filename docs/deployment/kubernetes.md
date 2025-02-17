# Kubernetes

This guide will walk through deploying Featureform on Kubernetes. The Featureform ingress currently supports AWS load balancers.&#x20;

## Prerequisites

* An existing Kubernetes Cluster in AWS
* A domain name that can be directed at the Featureform load balancer

## Step 1: Add Helm repos

Add Certificate Manager and Featureform Helm Repos.&#x20;

```
helm repo add featureform https://storage.googleapis.com/featureform-helm/ 
helm repo add jetstack https://charts.jetstack.io 
helm repo update
```

## Step 2: Install Helm Charts

### Certificate Manager&#x20;

If Certificate Manager has not yet been installed, install it before installing Featureform.

```
helm install certmgr jetstack/cert-manager \
    --set installCRDs=true \
    --version v1.8.0 \
    --namespace cert-manager \
    --create-namespace
```

### Featureform

Install Featureform with the desired domain name. Featureform will automatically provision the public TLS certificate when the specific domain name is routed to the Featureform loadbalancer.

```
helm install <release-name> featureform/featureform \
    --set global.hostname=<your-domain-name>
    --set global.publicCert=true
```

### Custom helm flags

| Name                     | Description                                                                                                                                   |         Default         |
|--------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------|:-----------------------:|
| global.hostname          | The hostname where the cluster will be accessible. Required to terminate the TLS certificate for GRPC.                                        |       "localhost"       |
| global.version           | The Docker container tag to pull. The default value is overwritten  with the latest deployment version when pulling from artifacthub.         |         "0.0.0"         |
| global.repo              | The Docker repo to pull the images from.                                                                                                      |    "featureformcom"     |
| global.pullPolicy        | The container pull policies.                                                                                                                  |        "Always"         |
| global.localCert         | Will create a self-signed certificate for the hostname. Either localCert or publicCert must be enabled if generating a certifiate.            |         "true"          |
| global.publicCert        | Whether to use a public TLS certificate or a self-signed on. If true, the public certificate is generated for the provided global.hostname.   |         "false"         |
| global.tlsSecretName     | Will set the name of the TLS secret for the ingress to use if manually adding a certificate.                                                  | "featureform-ca-secret" |
| global.k8s_runner_enable | If true, uses a Kubernetes Job to run Featureform jobs. If false, Featureform jobs are run in the coordinator container in a separate thread. |         "false"         |
| global.nginx.enabled     | Will install nginx along with Featureform if true.                                                                                            |         "true"          |
| global.logging                  | Will enable logging fluentbit, loki, and graphana within the cluster.                                                                         |         "true"          |

## Step 3: Domain routing

After Featureform has created its load balancer, you can create a CNAME record for your domain that points to the Featureform load balancer.&#x20;

Public TLS certificates will be generated automatically once the record has been created.
