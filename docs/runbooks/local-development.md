# Локальная разработка

## Быстрые проверки

```bash
make test-unit
make test
make build build-simulator
```

`make test` автоматически устанавливает закреплённые `controller-gen`, `setup-envtest` и Kubernetes envtest assets в `bin/`.

## Демонстрационный Kind-кластер

```bash
make dev-up
make dev-demo
kubectl get techprocess -n factory-demo -w
```

`dev-up` создаёт одновузловой стенд, на котором совмещены capability камеры и тестера. `dev-demo` собирает и загружает локальные образы, разворачивает оператор и применяет процессы `board-a`/`board-b`.

Полная топология с отдельными worker-узлами запускается так:

```bash
sudo sysctl -w fs.inotify.max_user_instances=1024 fs.inotify.max_user_watches=524288
make dev-up-multinode
make dev-demo
```

Повышение inotify требуется, если kubelet завершается с `too many open files`. Настройка через `sysctl -w` действует до перезагрузки.

Для переключения на второй профиль:

```bash
kubectl patch techprocess line-one -n factory-demo --type merge \
  -p '{"spec":{"templateRef":{"name":"board-b"},"parameters":{"product":"board-b","inspection-threshold":"0.97"}}}'
```

Прямой `kubectl patch` допустим только в локальной среде. Целевой production-путь будет выполнять такое изменение через Git и Argo CD.

Удаление локального стенда:

```bash
make dev-down
```
