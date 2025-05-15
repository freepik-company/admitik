# Admitik

## Deployment

We have designed the deployment of this project to allow remote deployment using Kustomize or Helm. This way it is possible
to use it with a GitOps approach, using tools such as ArgoCD or FluxCD.

If you prefer Kustomize, just make a Kustomization manifest referencing
the tag of the version you want to deploy as follows:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- https://github.com/freepik-company/admitik/releases/download/v0.1.0/install.yaml
```

> 🧚🏼 **Hey, listen! If you prefer to deploy using Helm, go to the [Helm registry](https://freepik-company.github.io/admitik/)**


## Flags

Some configuration parameters can be defined by flags that can be passed to the controller.
They are described in the following table:

| Name                                   | Description                                                                    |        Default         |
|:---------------------------------------|:-------------------------------------------------------------------------------|:----------------------:|
| `--metrics-bind-address`               | The address the metric endpoint binds to. </br> 0 disables the server          |          `0`           |
| `--health-probe-bind-address`          | he address the probe endpoint binds to                                         |        `:8081`         |
| `--leader-elect`                       | Enable leader election for controller manager                                  |        `false`         |
| `--metrics-secure`                     | If set the metrics endpoint is served securely                                 |        `false`         |
| `--enable-http2`                       | If set, HTTP/2 will be enabled for the metrirs                                 |        `false`         |
| `--sources-time-to-resync-informers`   | Interval to resynchronize all resources in the informers                       |         `60s`          |
| `--webhook-client-hostname`            | The hostname used by Kubernetes when calling the webhooks server               | `webhooks.admitik.svc` |
| `--webhook-client-port`                | The port used by Kubernetes when calling the webhooks server                   |        `10250`         |
| `--webhook-client-timeout`             | The seconds until timout waited by Kubernetes when calling the webhooks server |          `10`          |
| `--webhook-server-port`                | The port where the webhooks server listens                                     |        `10250`         |
| `--webhook-server-path`                | The path where the webhooks server listens                                     |      `/validate`       |
| `--webhook-server-ca`                  | The CA bundle to use for the webhooks server                                   |          `-`           |
| `--webhook-server-certificate`         | The Certificate used by webhooks server                                        |          `-`           |
| `--webhook-server-private-key`         | The Private Key used by webhooks server                                        |          `-`           |
| `--kube-client-qps`                    | The QPS rate of communication between controller and the API Server            |          `5`           |
| `--kube-client-burst`                  | The burst capacity of communication between the controller and the API Server  |          `10`          |

## How to develop

### Prerequisites
- Kubebuilder v4.0.0+
- go version v1.22.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### The process

> We recommend you to use a development tool like [Kind](https://kind.sigs.k8s.io/) or [Minikube](https://minikube.sigs.k8s.io/docs/start/)
> to launch a lightweight Kubernetes on your local machine for development purposes

For learning purposes, we will suppose you are going to use Kind. So the first step is to create a Kubernetes cluster
on your local machine executing the following command:

```console
kind create cluster
```

Once you have launched a safe play place, execute the following command. It will install the custom resource definitions
(CRDs) in the cluster configured in your ~/.kube/config file and run Kuberbac locally against the cluster:

```console
make install run
```

> [!IMPORTANT]
> When executing this, a temporary public reverse tunnel will be created.
> It goes from Kube Apiserver to your local webhooks server. It's done this way to be able to test the webhooks server
> using local development tools such as **Kind**

If you would like to test the operator against some resources, our examples can be applied to see the result in
your Kind cluster

```sh
kubectl apply -k config/samples/
```

> Remember that your `kubectl` is pointing to your Kind cluster. However, you should always review the context your
> kubectl CLI is pointing to


## How releases are created

Each release of this operator is done following several steps carefully in order not to break the things for anyone.
Reliability is important to us, so we automated all the process of launching a release. For a better understanding of
the process, the steps are described in the following recipe:

1. Test the changes on the code:

    ```console
    make test
    ```

   > A release is not done if this stage fails


2. Define the package information

    ```console
    export VERSION="0.0.1"
    export IMG="ghcr.io/freepik-company/admitik:v$VERSION"
    ```

3. Generate and push the Docker image (published on Docker Hub).

    ```console
    make docker-build docker-push
    ```

4. Generate the manifests for deployments using Kustomize

   ```console
    make build-installer
    ```
