# Admitik

<img src="https://raw.githubusercontent.com/achetronic/admitik/master/docs/img/logo.png" alt="Admitik Logo (Main) logo." width="150">

![GitHub go.mod Go version (subdirectory of monorepo)](https://img.shields.io/github/go-mod/go-version/freepik-company/admitik)
![GitHub](https://img.shields.io/github/license/freepik-company/admitik)

![YouTube Channel Subscribers](https://img.shields.io/youtube/channel/subscribers/UCeSb3yfsPNNVr13YsYNvCAw?label=achetronic&link=http%3A%2F%2Fyoutube.com%2Fachetronic)
![GitHub followers](https://img.shields.io/github/followers/achetronic?label=achetronic&link=http%3A%2F%2Fgithub.com%2Fachetronic)
![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/achetronic?style=flat&logo=twitter&link=https%3A%2F%2Ftwitter.com%2Fachetronic)


A dynamic Kubernetes admission controller that validates or modifies resources based on conditions you define using Helm templates.

It can retrieve data from other resources and inject it into templates, allowing for customizable conditions and messages.



## Motivation

Kubernetes has not an implementation of an admission controller to validate/mutate resources.
Instead, it leverages its creation to the clusters administrators.

This project was initiated to fill that gap by providing a potent and flexible admission controller.
It allows for dynamic validation and mutation of resources, using data from other resources and
enabling conditions and messages defined through Helm templates.

Our goal is to equip administrators with a tool that offers greater control and adaptability
in managing Kubernetes resources due to:

1. There are Kubernetes clusters where resources are introduced from a multitude of sources,
   where maintaining harmony and preventing conflicts in production environments is a significant challenge.

   It's essential to have a comprehensive set of policies that can enforce rules and prevent
   resource collisions effectively.

2. Existing solutions often fall short—they may not offer the level of power and dynamism
   necessary to address complex deployment scenarios.



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
| `--sources-time-to-reconcile-watchers` | Time between each reconciliation loop of the watchers                          |         `10s`          |
| `--sources-time-to-ack-watcher`        | Wait time before marking a watcher as acknowledged (ACK) after it starts       |          `2s`          |
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


## Examples

After deploying this operator, you will have new resources available. Let's talk about them.

> [!IMPORTANT]
> We are already creating a documentation page to explain all the features better

> > [!TIP]
> You can find examples for all the features of the resource in the [examples directory](./config/samples)

### How to create kubernetes dynamic admission policies

To create a dynamic admission policy in your cluster, you will need to create a `ClusterAdmissionPolicy` resource.
You may prefer to learn directly from an example, so let's explain it creating a ClusterAdmissionPolicy:

> [!IMPORTANT]
> When you have multiple ClusterAdmissionPolicy resources with the same watchedResources parameters,
> a resource can be rejected due to conditions specified in any of these policies.
>
> However, because conditions are evaluated one after the other, the rejection message displayed
> will be the one defined in the specific ClusterAdmissionPolicy where the rejecting condition is located.

```yaml
apiVersion: admitik.freepik.com/v1alpha1
kind: ClusterAdmissionPolicy
metadata:
   name: watch-resources
spec:
   # (Optional) Action to perform with the conditions are not met
   # Posible values: Enforce, Permissive
   # Enforce: (default) Reject the object.
   # Permissive: Accept the object
   # Both results create an event in Kubernetes
   failureAction: Enforce

   # Resources to be validated against the webhooks server.
   # Those matching any combination of following params will be sent.
   # As a hint: don't set operations you don't need for a resource type
   watchedResources:
      group: gateway.networking.k8s.io
      version: v1
      resource: httproutes
      operations:
         - CREATE
         - UPDATE

   # ALL the conditions must be valid to allow the resource entrance.
   conditions:
      - name: the-name-of-the-condition
        
        # The 'key' field is the place to write your template or code
        # The result of this field will be compared with 'value' for equality
        key: |
           # YOUR TEMPLATE HERE
           
        value: "your-value-here"

   message:
      template: |
         {{- printf "Reason behind rejection of the resource. This will be shown to the user" -}}
```

The mission is to create conditions whose templates, under _key_ field, output something. For each condition, 
the output of the template will be compared to the _value_ field. 
If they match, condition is met and the next one will be evaluated.

When a condition is NOT met, the object trying to enter in Kubernetes is rejected, and the _message_ is thrown to the user.

Let's focus on templates capabilities for conditions and message.
Templates can be written using several engines, such as _gotmpl_ or _starlark_ (maybe more in the future). We decided
to support several syntax options to give freedom to cluster operators. To select between them, setting `engine: gotmpl` 
or `engine: starlark` is the only thing you need.

> [!IMPORTANT]
> Policies without information are useless, right? To solve that, the controller injects data that are super
> useful to craft your policies:
>
> * `operation`: The operation that is being performed: create, update, etc
> * `object`: The object trying to enter to Kubernetes
> * `oldObject`: The previous existing object for those that are being updated
> * `sources`: The sources reclaimed by you in policy's `spec.sources` field. 
>   - For Gophers, this field is a `map[int][]any` 
>   - For Starlarkers this fields is a `dict(list(object))` 😵‍💫
> 
> For each template engine will be injected in their native way

#### Gotmpl

Gotmpl engine is Golang template with vitamins: basically, Golang template with several super useful functions added. 
This kind of template is better known as Helm template, so **all the functions available in Helm are available here too.**
This way you start creating wonderful policies from first minute.

```yaml
apiVersion: admitik.freepik.com/v1alpha1
kind: ClusterAdmissionPolicy
metadata:
  name: gotmpl-conditions
spec:

  # ... 
  
  conditions:
    - name: first-condition
      engine: gotmpl
      key: |
        {{- $someDataFromSomewhere := dict "name" "example-name" "namespace" "example-namespace" -}}
         
        {{- if eq $someDataFromSomewhere.name "example-name" -}}
          {{- printf "condition-is-met" -}} 
        {{- end -}} 
      value: "condition-is-met"
      
      
    - name: second-condition
      engine: gotmpl
      key: |
         {{- /* Some data are injected as previously mentioned */ -}}
         {{- /* Let's store them into variables */ -}}
         {{- $operation := .operation -}}
         {{- $oldObject := .oldObject -}}
         {{- $object := .object -}}
         {{- $sources := .sources -}}

         {{- if eq $object.metadata.name "example-name" -}}
           {{- printf "condition-is-met" -}} 
         {{- end -}} 
      value: "condition-is-NOT-met"


  message:
    template: |
      {{- $object := .object -}}
      {{- printf "Resource '%s' was rejected as some of declared routes already exists" $object.metadata.name -}}

```

Not only Helm-provided vitamins are available in `gotmpl` engine, on top of it we added some useful functions 
such as `setEnv` and `logPrintf`, let's explain a bit. 

**Sometimes you need to store information** during `gotmpl` conditions' evaluation. This is useful to keep and populate 
some data that were generated in a `gotmpl` condition to another. In that situations, it's possible to use `setEnv`


```yaml
apiVersion: admitik.freepik.com/v1alpha1
kind: ClusterAdmissionPolicy
spec:

  # ...

  conditions:
    - name: store-vars-for-later-usage
      key: |
        {{- $someDataForLater := dict "name" "example-name" "namespace" "example-namespace" -}}

        {{/* Store your data under your desired key. You can use as many keys as needed */}}
        {{- setVar "some_key" $someDataForLater -}}

        {{- printf "force-condition-not-being-met" -}}
      value: "condition-key-result"

  message:
    template: |
      {{- $myVars := .vars -}}

      {{- $someKeyInside := $myVars.some_key-}}

      {{- printf "let's show some stored data: %s/%s" $someKeyInside.name $someKeyInside.namespace -}}
```

> [!TIP]
> Another useful function that can be used in templates is `logPrintf`. It accepts the same params as printf
> but throw the result in controller's logs instead of returning it 

#### Starlark

Oh, so you prefer Starlark instead of Gotmpl? you are betraying Go's community wanting something different. Anyway,
we have you covered with wonderful Starlark conditions:

```yaml
apiVersion: admitik.freepik.com/v1alpha1
kind: ClusterAdmissionPolicy
metadata:
  name: starlark-conditions
spec:

  # ... 
  
  conditions:
    - name: first-condition
      engine: starlark
      key: |
        # Injected data is located in following global variables:
        # operation, oldObject, object, sources, vars
         
        print(operation)
      value: "UPDATE"
      
      
    - name: second-condition
      engine: starlark
      key: |
        print(object["kind"])
      value: "Deployment"
      
      
    - name: third-condition
      engine: starlark
      key: |
         
        # You can even define functions
        def findObjectInSources (sources):
          for key in sources:
            subList = sources[key]
            for obj in subList:
              if obj["kind"] == "Deployment" && obj["metadata"]["name"]:
                print("ObjectFound")
              else:
                print("ObjectNotFound")

        findObjectInSources(sources)
      value: "ObjectFound"

  message:
    engine: starlark 
    template: |
      print("Resource '{}' was rejected as some condition is not met".format(object["metadata"]["name"]))
```

It's even possible to transmit information between your conditions and message using `vars` global variable:

```yaml
apiVersion: admitik.freepik.com/v1alpha1
kind: ClusterAdmissionPolicy
metadata:
  name: starlark-conditions
spec:

  # ... 
  
  conditions:
    - name: first-condition
      engine: starlark
      key: |        
        vars.update({"your-key": "your-value"})
        vars.update({"your-other-key": ["what", "ever", "you", "need"]})
         
        print(operation)
      value: "UPDATE"
      
    - name: second-condition
      engine: starlark
      key: |
         # vars["your-key"] has 'your-value' inside
         # Let's show all of them by logs
         log.printf("Available variables: {}".format(vars))
         
         print(vars["your-key"])
      value: "your-value"

  message:
    engine: starlark 
    template: |
      print("Resource '{}' was rejected as some condition is not met".format(object["metadata"]["name"]))
```

You can see all you need in these helpful links: 
* [Syntax and Functions](https://starlark-lang.org/spec.html)
* [Extra official supported libs](https://github.com/google/starlark-go/tree/master/lib)
* [Extra unofficial supported libs](https://github.com/freepik-company/admitik/tree/master/internal/template/starlarkmods)
* [Playground](https://starlark-lang.org/playground.html)

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



## How to collaborate

This project is done on top of [Kubebuilder](https://github.com/kubernetes-sigs/kubebuilder), so read about that project
before collaborating. Of course, we are open to external collaborations for this project. For doing it you must fork the
repository, make your changes to the code and open a PR. The code will be reviewed and tested (always)

> We are developers and hate bad code. For that reason we ask you the highest quality on each line of code to improve
> this project on each iteration.



## License

Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
