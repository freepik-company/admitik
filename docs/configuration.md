# Configuration

## Flags

Some configuration parameters can be defined by flags that can be passed to the controller.
They are described in the following table:

| Name                                 | Description                                                                    |        Default         |
|:-------------------------------------|:-------------------------------------------------------------------------------|:----------------------:|
| `--metrics-bind-address`             | The address the metric endpoint binds to. </br> 0 disables the server          |          `0`           |
| `--health-probe-bind-address`        | he address the probe endpoint binds to                                         |        `:8081`         |
| `--leader-elect`                     | Enable leader election for controller manager                                  |        `false`         |
| `--metrics-secure`                   | If set the metrics endpoint is served securely                                 |        `false`         |
| `--enable-http2`                     | If set, HTTP/2 will be enabled for the metrirs                                 |        `false`         |
| `--sources-time-to-resync-informers` | Interval to resynchronize all resources in the informers                       |         `60s`          |
| `--webhook-client-hostname`          | The hostname used by Kubernetes when calling the webhooks server               | `webhooks.admitik.svc` |
| `--webhook-client-port`              | The port used by Kubernetes when calling the webhooks server                   |        `10250`         |
| `--webhook-client-timeout`           | The seconds until timout waited by Kubernetes when calling the webhooks server |          `10`          |
| `--webhook-server-port`              | The port where the webhooks server listens                                     |        `10250`         |
| `--webhook-server-path`              | The path where the webhooks server listens                                     |      `/admission`      |
| `--webhook-server-ca`                | The CA bundle to use for the webhooks server                                   |          `-`           |
| `--webhook-server-certificate`       | The Certificate used by webhooks server                                        |          `-`           |
| `--webhook-server-private-key`       | The Private Key used by webhooks server                                        |          `-`           |
| `--kube-client-qps`                  | The QPS rate of communication between controller and the API Server            |          `5`           |
| `--kube-client-burst`                | The burst capacity of communication between the controller and the API Server  |          `10`          |
| `--enable-special-labels`            | Enable labels that perform sensitive actions                                   |        `false`         |
| `--exclude-admission-self-namespace` | Exclude Admitik resources from admission evaluations                           |        `false`         |
| `--excluded-admission-namespaces`    | Comma-separated list of namespaces to be excluded from admission evaluations   |          `-`           |
