---
title: "Introduction to Bazel"
date: 2026-04-01
tags: ["bazel", "build-systems"]
summary: "Exploring bazel build system."
---

> Note: this post was originally written in 2024 and updated in April 2026 to reflect some Bazel ecosystem changes that happened since then.

In this post I want to talk about how we build and test our software. It seems like an obvious question, which is exactly why we tend to overlook it - just take your favourite build tool (gradle, maven, sbt or any language specific build tool) and start tinkering around with your project. But the question is - can we do better? What's wrong with existing tools? In this post I'll try to give an overview of bazel build tool, explain main ideas behind it and show how it can increase productivity of your team.

## What is Bazel? 

Bazel is a modern build and test tool, similar to the tools that you're used to. On the first glance it does exactly the same job, but the ideas behind the processes are quite different. 

Bazel has been originally developed in Google (known as `blaze`) and open sourced several years ago. It's main focus was to solve some problems that Google had with [their monorepository](https://cacm.acm.org/magazines/2016/7/204032-why-google-stores-billions-of-lines-of-code-in-a-single-repository/fulltext), here are some of them:

- maintaining large code base at scale,
- slow compilation time,
- slow execution time of unit tests,
- support for multiple languages,
- dependency management across services,
- universal tools for building and testing projects.

Even though I'm a huge fan of monorepos, we're not going to discuss here all advantages and trade-offs of this code structuring type. Instead, let's dive into more details about bazel itself :)

## Challenges with traditional build tools

First of all let's discuss why somebody might want to have a new build tool.

### Slow compilation time 
As the company grows its codebase will grow as well. And obviously, the more code you have to build and maintain - the more time it will take. Given that a lot of compilers (C++, Rust, Scala with sbt) are pretty slow (mostly for a good reason!), compilation time becomes very important factor for developers productivity and development velocity. One of the solutions is to use incremental compilation, which in my experience it's not reliable enough and could easily produce some cryptic run-time bugs (like `java.lang.ClassNotFoundException`) which only go away once you rebuild everything from scratch. 

### Non-reproducible builds
One of the most important things is that almost all build tools depends on your system environment and configuration. If you build your project with JDK 25 and your remote colleague accidentally has JDK 18, most likely project will not compile. Same thing could happen with GCC version or any other global dependency that you may have installed on your host machine or CI server. **Therefore, your builds are not reproducible and not deterministic.** From the same code base you can get different results and service that works on your machine can suddenly start throwing exceptions on production. And ultimately it becomes harder to guarantee software quality, because it can easily lead to random or tricky bugs that are hard to reproduce locally.

### Different build tools and their complexity
Almost every programming language brings its own build and test tools which is not bad by itself. But some of these tools might be complicated and even confusing. As an example we might think about standard build tool in Scala community called `sbt`. If you're familiar with this tool, then you'd probably know how hard it is for beginners to start using it and how difficult it is to maintain and evolve configuration. I'll not dive here into all details, but if you're interested in it - [there is good article](http://www.lihaoyi.com/post/SowhatswrongwithSBT.html) with deep analysis.

On the top of that, there is also a pretty common case in companies to have different tech stacks across teams, so every time when developer jumps to another project or wants to help other team, they most likely need to learn a new toolchain, which increases the learning curve and entry threshold.

## Bazel to the rescue
Now, let's have a look into main concepts and ideas in Bazel. When you install bazel for the first time, it doesn't know anything about how to build your project. To be able to compile and package your project you have to plug-in special `rules`. 

Rule in `bazel` is a special definition that describes the relationship between inputs and outputs, and the steps to build those outputs. If you're familiar with functional programming concepts you can treat every rule as a pure function that takes inputs (e.g. code sources) and produces some outputs (e.g. binaries, but actually it could be whatever you want). It's also lazy, meaning that it won't be executed until you actually start building process and explicitly call it. 

Bazel community does an amazing work and [provides a lot of ready out of the box rules](https://github.com/bazelbuild) for building and testing apps for specific programming languages and even frameworks, so you can easily build Java, Scala, Rust, Haskell, Go, JavaScript, Protobuf & gRPC and more! 

The "purity" of rules leads us to another beautiful concept and this is where `bazel` really starts to shine. And it's called hermetic builds.

### Hermetic builds
In simple words hermetic build means that build process doesn’t depend on host environment, it doesn't have access to env variables, installed software like compilers, SDKs, or libraries — no assumptions are made that something already exists and properly configured on hosting machine (e.g. protobuf compiler) or docker daemon. We have complete sandbox with deterministic actions that in theory always guarantees reproducibility. There is no more need for complicated host configurations and pain for newcomers - everything that your project need for build is defined inside your build files. Sandboxing also helps to prevent build system bugs itself, which is common especially for incremental builds and complex build systems. 

But how does it work? Basically, `bazel` computes hashes for every input and output and since we know all dependencies for every task in action graph, and we don’t have any implicit assumptions on the system configuration, bazel can guarantee that every time we run build command the output will be exactly the same. Ideally, matching it bit-to-bit - which bazel strives for but doesn't universally guarantee. 

This fact gives us an extreme power: first of all, we can cache all outputs and if inputs don't change - we can just skip building it again and put it into local or remote cache. And as the codebase becomes very large, rebuilding it from scratch can take significant amount of time or even could not be possible to complete on local machines. However, with remote cache most part is already there - we only need to recompile targets that have changed. The same thing happens with unit tests - if covered code has not been changed, there is no need to test it again. Therefore we can benefit a lot from extremely fast local and CI builds (with our Scala monorepo we got more than 15x speedup!).  Moreover, if your company approaches Google-scale size and code base, you can also benefit from distributed builds.

Another interesting thing that we can deliver exactly the same application version for development, QA and deployment. If this sounds like a Docker use case — you're right, but `bazel` gives you an ability to achieve it without additional tools. Moreover, docker builds are not reproducible by default either (more details about that in following parts).

If you use microservice architecture for your backend, combined with mono-repository it’s very easy to have a snapshot of your system for every developing step. 

### Dependency management
The good thing about `bazel` is that it supports the concept of hermetic builds so thoroughly that it’s almost impossible to create a non-hermetic build. But of course, this requires some discipline that we have to follow in order to achieve it. 

First of all, we need to specify our third party dependencies such as Java artifacts in a bit specific way: not only do we need to specify versions and artifacts location, but we also need to provide hash for them to make sure that nobody has republished different code with the same version. This is especially critical for dynamic languages such as JavaScript with `npm` or Python.

The second important thing - at the build target level, we need to explicitly declare all transitive dependencies for each target (e.g. if your `java_binary` depends on a library that in turn depends on another, both must be listed). It requires more effort from developers, but it solves a lot of problems so in my opinion it’s totally worth it. Of course nobody wants to do it manually, so Bazel developers provide us special tools to generate dependency lists for different languages (e.g. [rules_jvm_external](https://github.com/bazel-contrib/rules_jvm_external) has special support for Maven dependencies via lock files).

To make our life a bit easier, `bazel` also contains a couple of auxiliary commands, so we can write special queries (via `bazel query` syntax) and track down dependencies for our targets. It uses special syntax for graph description so you can even visualise it using tool like `Graphviz`. It's extremely useful tool for tracking dependencies among services and protocol changes (in case if you use something like `gRPC`). 

## Pitfalls
Like any other technology, Bazel is not perfect. Here are some issues I’ve encountered while working with it.

First, while Bazel's core is mature, community-maintained rules can sometimes lack documentation or have unspecified properties. Be ready to read rules source code and ask the community for help.

Second, you should get used to dependencies declaration and always keep in mind the main idea of Bazel - ensure reproducible builds. The hermeticity mindset can surface in unexpected places, such as needing to explicitly handle environment variables for staging and production deployments or explicitly declare resource files (e.g. a bunch of JSON examples) for unit tests in the build file.

Third, many Bazel rules are supported by the community, so be prepared to contribute or spend extra time trying to achieve some non-standard stuff. For example, most of the rules support compilation and testing, but it can be difficult to get test coverage reports. Similarly, getting [support for custom `TypeMappers` in protobuf](https://github.com/bazel-contrib/rules_scala/issues/751) can require community effort. 

All those factors could prevent one from going all-in on a Bazel build setup, because languages are highly ingrained in their environments (Rust with Cargo, npm for JS) and it could be challenging to match dev experience with a universal build system.

## Summary
Overall, I think Bazel is an awesome tool and its benefits definitely outweigh the downsides. 

I've been using it at [XITE](https://xite.com) for our mono-repository setup since 2019 with mixed Scala, Java and Python languages, and while setting up and maintaining such a setup is not trivial it definitely earns its keep with fast build iterations and CI.

So I would definitely recommend to play around with this tool and give it a try!

In a future post I'll take a closer look at the practical side of Bazel: initial build configuration, build files, compilation and testing. 

## Useful links
- [Official bazel website](https://bazel.build/)
- [Bazel vision](https://docs.bazel.build/versions/master/bazel-vision.html)
- [Bazel ecosystem (GitHub)](https://github.com/bazelbuild)