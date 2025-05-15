# Admitik

<img src="https://raw.githubusercontent.com/achetronic/admitik/master/docs/img/logo.png" alt="Admitik Logo (Main) logo." width="150">

![GitHub go.mod Go version (subdirectory of monorepo)](https://img.shields.io/github/go-mod/go-version/freepik-company/admitik)
![GitHub](https://img.shields.io/github/license/freepik-company/admitik)

![YouTube Channel Subscribers](https://img.shields.io/youtube/channel/subscribers/UCeSb3yfsPNNVr13YsYNvCAw?label=achetronic&link=http%3A%2F%2Fyoutube.com%2Fachetronic)
![GitHub followers](https://img.shields.io/github/followers/achetronic?label=achetronic&link=http%3A%2F%2Fgithub.com%2Fachetronic)
![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/achetronic?style=flat&logo=twitter&link=https%3A%2F%2Ftwitter.com%2Fachetronic)


**Admitik** is a cloud native **policy engine** for Kubernetes that lets you define policies 
to **validate**, **mutate**, **generate**, **clone**, or **clean** resources. 

It uses template engines (like CEL or Starlark) to apply logic, patch resources, or generate new ones 
— all directly inside your cluster.

No new languages to learn. No sidecars. Just Kubernetes-native power. 💪


## ✨ What Can Admitik Do?

- ✅ **Validation** Allow or block resources like Pods, Namespaces, etc., based on your logic.
- 🔁 **Mutation** Automatically add labels, annotations, or patch fields before resources are stored.
- 📦 **Generation** Create new resources when something happens — like generating a `ConfigMap` when a `Namespace` appears.
- 🧬 **Cloning** Copy existing resources into other namespaces.
- 🧹 **Cleanup** Delete resources that match custom conditions (e.g. old `Jobs` or temp objects).


## 🧰 Template Engines

Admitik uses templating to evaluate conditions, build messages, craft patches, or define generated objects.

Supported engines:

- 🧾 **Go Templates** (with [Sprig functions](https://masterminds.github.io/sprig/))
- 🔢 **CEL** (Common Expression Language)
- 🐍 **Starlark** (a Python-like scripting language)
- 💡 **Plain+CEL** (light templating with inline CEL expressions)

Choose the one that fits your needs — or combine them in the same policy!


## 🧠 Template Evaluation Context

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
> Remember that each engine has its own capabilities, so all the variables are always available, 
> but not all engines can do everything. For example, CEL is simple so can't write in `vars`


## 📂 Policy Kinds

| Kind                      | What it does                                          |
|---------------------------|-------------------------------------------------------|
| `ClusterValidationPolicy` | Validates intercepted resources                       |
| `ClusterMutationPolicy`   | Modifies intercepted resources                        |
| `ClusterGenerationPolicy` | Generates new resources (or clone existing) on events |
| `ClusterCleanupPolicy`    | Deletes resources under custom rules                  |


## 🧪 Examples

We’ve prepared real-world examples so you can get started quickly:

<!---
HIDDEN UNTIL DOC PAGES ARE CRAFTED
👉 [admitik.dev/docs/examples](https://admitik.dev/docs/examples)
-->

[Examples](./docs/samples)


## 📦 Installation

We will cover all the installation methods in documentation soon, in the meanwhile, instructions here!

[Helm registry](https://freepik-company.github.io/admitik/)

<!---
HIDDEN UNTIL DOC PAGES ARE CRAFTED

## 🌐 Documentation

Advanced usage guides, examples, and reference docs coming soon:

👉 [admitik.dev/docs](https://admitik.dev/docs)
-->


## 🤝 Contributing

All contributions are welcome! Feel free to:

- Open issues
- Send pull requests
- Ask questions
- Suggest features

## 💬 Need Help?

Start a [discussion](https://github.com/freepik-company/admitik/discussions) or visit [issues](https://github.com/freepik-company/admitik/issues).


## 📄 License

Admitik is licensed under the [Apache 2.0 License](./LICENSE).
