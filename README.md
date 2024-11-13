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



## Examples

After deploying this operator, you will have new resources available. Let's talk about them.

### How to create kubernetes dynamic admission policies

To create a dynamic admission policy in your cluster, you will need to create a `ClusterAdmissionPolicy` resource.
You may prefer to learn directly from an example, so let's explain it creating a ClusterAdmissionPolicy:

> [!TIP]
> You can find the spec samples for all the versions of the resource in the [examples directory](./config/samples)

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
  labels:
    app.kubernetes.io/name: admitik
    app.kubernetes.io/managed-by: kustomize
  name: avoid-colisioning-routes
spec:

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

  # Other resources to be retrieved for later templates.
  # They will be included under .sources scope for conditions and message
  sources:
    - group: gateway.networking.k8s.io
      version: v1
      resource: httproutes

      # (Optional) It's possible to retrieve specific resources
      # name: secondary-route
      # namespace: default


  # ALL the conditions must be valid to allow the resource entrance.
  conditions:
    - name: confirm-non-existing-routes

      # The 'key' field admits vitamin Golang templating (well known from Helm)
      # The result of this field will be compared with 'value' for equality
      key: |
        {{- $operation := .operation -}}
        {{- $object := .object -}}
        {{- $oldObject := .oldObject -}}
        {{- $sources := .sources -}}


        {{- $routeFound := false -}}

        {{- $routes := (index .sources 0) -}}
        {{- range $routeObjIndex, $routeObj := $routes -}}

          {{/* Here some logic to confirm you found the route already existing */}}
          {{- $routeFound := true -}}

        {{- end -}}

        {{- if $routeFound -}}
          {{- printf "route-already-created" -}}
        {{- else -}}
          {{- printf "route-not-found" -}}
        {{- end -}}

      value: "route-not-found"

  message:
    template: |
      {{- $object := .object -}}
      {{- printf "Resource '%s' was rejected as some of declared routes already exists" $object.metadata.name -}}

```

As you probably noticed in the previous example, conditions are made using vitamin Golang template
(better known as Helm template), so **all the functions available in Helm are available here too.**
This way you start creating wonderful policies from first minute.

**Sometimes you need to store information** during conditions' evaluation that will be useful in later messages shown to the team.
This will help your mates having meaningful messages that save time during debug.

Because of that, there is a special function available in templates called `setVar`. It can be used as follows:

```yaml
apiVersion: admitik.freepik.com/v1alpha1
kind: ClusterAdmissionPolicy
spec:

  ...

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
