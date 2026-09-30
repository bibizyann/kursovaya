# Инструменты проекта

Версии закрепляются в исходном коде проекта, чтобы сборка не зависела от случайного состояния рабочей станции.

| Инструмент | Версия/источник |
|---|---|
| Go | `go 1.26.0` в `go.mod`, автоматический patch toolchain |
| Kubebuilder | `4.15.0` в `PROJECT` |
| controller-runtime | `0.24.1` в `go.mod` |
| Kubernetes API/client-go | `0.36.0` в `go.mod` |
| controller-gen | `0.21.0` в `Makefile` |
| setup-envtest | версия controller-runtime, вычисляется из `go.mod` |
| envtest Kubernetes | `1.36`, вычисляется из версии `k8s.io/api` |
| kubectl | `1.36.0`, официальный бинарник с SHA-256 |
| kustomize | `5.8.1` в `Makefile` |
| golangci-lint | `2.12.2` в `Makefile` |
| Kind | проверено с `0.31.0` |
| Helm | проверено с `4.2.3`, Helm-чарт появится на этапе GitOps |

Установка всех управляемых проектом инструментов:

```bash
make tools
```

Бинарники и envtest assets помещаются в `bin/` и не коммитятся. Go build cache помещается в `.cache/`.

Docker, Kind и Helm пока считаются host-инструментами. Их версии проверяются перед изменением локальной инфраструктуры.

## Ограничение inotify для multi-node Kind

Если worker kubelet падает с `inotify_init: too many open files`, временно увеличьте лимиты:

```bash
sudo sysctl -w fs.inotify.max_user_instances=1024 fs.inotify.max_user_watches=524288
```

После этого используется `make dev-up-multinode`. Для повседневной разработки достаточно одновузлового `make dev-up`.
