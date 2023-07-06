# Purpose

This program is mostly for my own semi-public instance that uses a [slightly
modified code](https://github.com/rkfg/stable-diffusion-webui) that allows to
have frozen settings but still switch models and apply other parameters (such as
clip skip) for just one generation.

When I add multiple models to Stable Diffusion and want to make them available
with the model swapper they need to be hashed first. It's only possible to do by
hand, loading every checkpoint one by one. This program automates populating
cache.json so all I need is to restart the web UI after that.

```
Usage:
  sdhasher [OPTIONS]

Application Options:
  -p=         Path to the models directory
  -i=         Path to source cache.json file
  -o=         Path to resulting cache.json file
  -m=         Max number of hashing tasks

Help Options:
  -h, --help  Show this help message
  ```