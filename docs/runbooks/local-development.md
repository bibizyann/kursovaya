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

`dev-up` создаёт одновузловой стенд, на котором совмещены capability всех шести рабочих мест. `dev-demo` собирает и загружает локальные образы, разворачивает оператор и применяет шаблоны `board-a`/`board-b` и активный процесс `line-one` с восемью симуляторами.

Если старый development-кластер уже существует, `dev-up` не обновит его метки. Можно создать отдельный стенд с новой конфигурацией, сохранив старый кластер и kubeconfig:

```bash
mkdir -p .cache
export KUBECONFIG="$PWD/.cache/assembly-kubeconfig"
make dev-demo DEV_CLUSTER=factory-assembly-dev
kubectl get pods -n factory-demo -o wide
```

Переменную `KUBECONFIG` нужно сохранять для последующих команд этого стенда. При повторной сборке образов с тем же тегом `:dev` уже работающие pod не обновляются автоматически: для эксперимента задайте уникальный тег и обновите `image` в шаблонах, загрузив этот образ через `SIMULATOR_IMG`.

Полная топология содержит worker для первых трёх SMT-операций, worker для контроля/ручной сборки/испытаний и симулируемый резерв со всеми возможностями. Она запускается в отдельном новом кластере так:

```bash
sudo sysctl -w fs.inotify.max_user_instances=1024 fs.inotify.max_user_watches=524288
make dev-up-multinode DEV_CLUSTER=factory-assembly-multi
make dev-demo DEV_CLUSTER=factory-assembly-multi
```

Повышение inotify требуется, если kubelet завершается с `too many open files`. Настройка через `sysctl -w` действует до перезагрузки.

Для переключения на второй профиль:

```bash
kubectl patch techprocess line-one -n factory-demo --type merge \
  -p '{"spec":{"templateRef":{"name":"board-b"},"parameters":{"product":"board-b"}}}'
```

Прямой `kubectl patch` допустим только в локальной среде. Целевой production-путь будет выполнять такое изменение через Git и Argo CD.

После смены шаблона оператор обновляет восемь Deployment. Сейчас обновления не упорядочены по рабочим местам; последовательный rollout и автоматический rollback ещё предстоит реализовать. [Инструкция симуляторов](equipment-simulation.md) описывает операции, готовность и инъекцию отказов.

Удаление стандартного локального стенда (для другого имени передайте соответствующий `DEV_CLUSTER`):

```bash
make dev-down
```
