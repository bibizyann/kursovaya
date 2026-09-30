# Factory Platform

Прототип информационной системы управления жизненным циклом ПО на распределённых вычислительных узлах сборочного цеха. Основной код написан на Go, а декларативная модель реализована через Kubernetes CRD и оператор.

Исходная постановка находится в [`Диплом.md`](./Диплом.md), сквозной roadmap — в [`План.md`](./План.md).

## Реализовано

- API `TechProcess` и `TechProcessTemplate` группы `production.factory.local/v1alpha1`;
- capability-based размещение через `LabelSelector` и node affinity;
- детерминированная revision, immutable ConfigMap параметров;
- создание и обновление `Deployment`/`DaemonSet`;
- безопасный `Recreate` по умолчанию;
- Conditions `Ready`, `Progressing`, `Degraded` и timeout rollout;
- HTTP-симулятор камеры/тестового стенда с управляемыми отказами;
- unit-тесты и интеграционные тесты контроллера через `envtest`;
- Kind-конфигурация с тремя edge-worker-узлами;
- GitLab CI и контейнерные сборки оператора/симулятора.

## Архитектурный поток

```text
UI -> Factory API -> Git -> Argo CD -> TechProcess -> Go operator -> workloads
                                                        |
edge agent <- local gRPC <- process workload             +-> status
     |
     +-> MQTT -> telemetry and monitoring
```

На текущем этапе прямое применение CR допускается для разработки. Целевой production-путь использует Git как единственный write channel.

## Требования

- Go toolchain из `go.mod` — загружается автоматически;
- Docker;
- `kubectl`;
- Kind;
- GNU Make.

`controller-gen`, `kustomize`, `setup-envtest` и `golangci-lint` устанавливаются локально в `bin/` закреплёнными версиями из `Makefile`.

## Проверки

```bash
make test-unit
make test
make build build-simulator
```

## Локальный стенд

```bash
make dev-up
make dev-demo
kubectl get techprocess -n factory-demo -w
```

По умолчанию используется одновузловой Kind-стенд. Полная топология с отдельными camera/tester/spare worker-узлами запускается командой `make dev-up-multinode` после повышения локальных inotify-лимитов; подробности приведены в runbook.

Подробная инструкция и переключение `board-a → board-b` описаны в [`docs/runbooks/local-development.md`](./docs/runbooks/local-development.md).

Удаление только development-кластера:

```bash
make dev-down
```

## Основные каталоги

```text
api/v1alpha1/        Kubernetes API
internal/controller Reconcile-цикл
internal/workload   Чистая генерация workload-ресурсов
internal/simulator  Симулятор оборудования
config/             CRD, RBAC, deployment и samples
infra/kind/         Локальная топология
docs/adr/           Архитектурные решения
```

## Лицензия

Apache License 2.0.
