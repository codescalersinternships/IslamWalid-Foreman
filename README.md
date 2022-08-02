# Foreman
It is a [foreman](https://github.com/ddollar/foreman) implementation in GO.

## Description
Foreman is a manager for [Procfile-based](https://en.wikipedia.org/wiki/Procfs) applications. Its aim is to abstract away the details of the Procfile format, and allow you to run your services directly.

## Features
- Run procfile-backed apps.
- Able to run with dependency resolution.

## Procfile
Procfile is simply `key: value` format like:
```yaml
app:
  cmd: ping -c 5 google.com 
  checks:
    cmd: sleep 1
  deps: 
      - redis
redis:
  cmd: redis-server --port 6010
  run_once: true
  checks:
    cmd: redis-cli -p 6010 ping
```
**Here** we defined two services `app` and `redis` with check commands and dependency matrix

## How to use
**First:** add the procfile with processes or services you want to run.

**second**: run with command: 
```sh
go run *.go
```
