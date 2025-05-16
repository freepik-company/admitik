# Admitik

**Cloud native policy engine for Kubernetes**

![GitHub go.mod Go version (subdirectory of monorepo)](https://img.shields.io/github/go-mod/go-version/freepik-company/admitik)
![GitHub](https://img.shields.io/github/license/freepik-company/admitik)

![YouTube Channel Subscribers](https://img.shields.io/youtube/channel/subscribers/UCeSb3yfsPNNVr13YsYNvCAw?label=achetronic&link=http%3A%2F%2Fyoutube.com%2Fachetronic)
![GitHub followers](https://img.shields.io/github/followers/achetronic?label=achetronic&link=http%3A%2F%2Fgithub.com%2Fachetronic)
![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/achetronic?style=flat&logo=twitter&link=https%3A%2F%2Ftwitter.com%2Fachetronic)

<img src="https://raw.githubusercontent.com/achetronic/admitik/master/docs/img/header.png" alt="Admitik Header (Main) logo." width="100%">


Admitik is a cloud native policy engine for Kubernetes that lets you define policies 
to **validate**, **mutate**, **generate**, **clone**, or **clean** resources. 

It uses template engines (like CEL or Starlark) to apply logic, patch resources, or generate new ones 
— all directly inside your cluster.

No new languages to learn. No sidecars. Just Kubernetes-native power. 💪


## ✨ What Can Admitik Do?

#### ✅ **Validation**
Enforce admission rules to keep your cluster secure, compliant, and predictable.

- Block configurations that violate security or runtime policies
- Enforce consistent naming, labeling, or structural patterns
- Reject resources that miss required platform standards (e.g. limits, roles, labels)

#### 🔁 **Mutation**
Modify resources before they’re stored to ensure they meet platform expectations.

- Auto-inject metadata for cost tracking, ownership, or auditing
- Add observability settings (e.g. monitoring annotations) automatically
- Apply missing defaults for scheduling, networking, or access behavior

#### 📦 **Generation**
Create complementary resources in response to cluster activity.

- Deploy baseline policies or controls when new environments appear
- Automatically provision RBAC or access scopes based on context
- Generate environment-specific configs to simplify onboarding

#### 🧬 **Cloning**

> [!IMPORTANT]
> We are working on this feature! 🛠️

<!---
Replicate trusted configurations across scopes to ensure alignment and reduce duplication.

- Distribute shared policies or settings across teams or namespaces
- Keep environments in sync by replicating structural patterns
- Copy access or config resources securely between isolated areas
-->

#### 🧹 **Cleanup**

> [!IMPORTANT]
> We are working on this feature! 🛠️

<!---
Continuously remove resources that are no longer relevant or safe to keep.

- Delete completed workloads to avoid clutter and resource waste
- Clean up temporary or short-lived artifacts after use
- Enforce retention policies for unused or expired infrastructure
-->


## 🧰 Template Engines

Admitik uses templating to evaluate conditions, build messages, craft patches, or define generated objects.

Supported engines:

- **Go Templates** (with [Sprig functions](https://masterminds.github.io/sprig/))
- **CEL** (Common Expression Language)
- **Starlark** (a Python-like scripting language)
- **Plain** (you write it, your rules)
- **Plain+CEL** (light templating with inline CEL expressions)

Choose the one that fits your needs — or combine them in the same policy!

<!---
### 🧠 Evaluation Context
-->

Inside any template, you can access these powerful variables:

| Key         | Description                                                                                         |
|-------------|-----------------------------------------------------------------------------------------------------|
| `object`    | The resource being created, updated, or deleted                                                     |
| `oldObject` | The previous version (on `UPDATE` operations)                                                       |
| `operation` | The current action: `CREATE`, `UPDATE`, or `DELETE`                                                 |
| `sources`   | Lists of extra Kubernetes resources you request for evaluation (like `ConfigMaps` or `Deployments`) |
| `vars`      | A shared dictionary to store and reuse values across conditions and templates                       |

These variables let you write dynamic, context-aware policies using real cluster data. 🔍

> [!TIP]
> Remember that each engine has its own capabilities, so all the variables are available everywhere, 
> but not all engines can do everything. For example, CEL is for simple expressions, so it can read `vars` but can not modify them


## 📂 Policy Kinds

| Kind                      | What it does                                          |
|---------------------------|-------------------------------------------------------|
| `ClusterValidationPolicy` | Validates intercepted resources                       |
| `ClusterMutationPolicy`   | Modifies intercepted resources                        |
| `ClusterGenerationPolicy` | Generates new resources (or clone existing) on events |

<!---
| `ClusterCleanupPolicy`    | Deletes resources under custom rules                  |
-->

## 🧪 Examples

We’ve prepared real-world examples so you can get started quickly:

<!---
HIDDEN UNTIL DOC PAGES ARE FULLY CRAFTED
👉 [admitik.dev/docs/examples](https://admitik.dev/docs/examples)
-->

[Examples](./docs/samples)


## 📦 Installation

We will cover all the installation methods in documentation soon, in the meanwhile, instructions here!

[Helm registry](https://freepik-company.github.io/admitik/)

<!---
HIDDEN UNTIL DOC PAGES ARE FULLY CRAFTED

## 🌐 Documentation

Advanced usage guides, examples, and reference docs coming soon:

👉 [admitik.dev/docs](https://admitik.dev/docs)
-->

## 🤝 Contributing

All contributions are welcome! Whether you're reporting bugs, suggesting features, or submitting code — thank you! Here’s how to get involved:

▸ [Open an issue](https://github.com/freepik-company/Admitik/issues/new) to report bugs or request features

▸ [Submit a pull request](https://github.com/freepik-company/Admitik/pulls) to contribute improvements

<!---
▸ [Ask a question or start a discussion](https://github.com/freepik-company/Admitik/discussions)
-->

▸ [Check open milestones](https://github.com/freepik-company/Admitik/milestones) to see what’s coming

▸ [Read the contributing guide](./docs/CONTRIBUTING.md) to get started smoothly


## 📄 License

Admitik is licensed under the [Apache 2.0 License](./LICENSE).
