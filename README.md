# Factory Platform

Прототип информационной системы управления жизненным циклом ПО на распределённых вычислительных узлах сборочного цеха. Основной код написан на Go, а декларативная модель реализована через Kubernetes CRD и оператор.

Исходная постановка находится в [`Диплом.md`](./Диплом.md), сквозной roadmap — в [`План.md`](./План.md).

Согласованная [модель сборочного цеха](docs/architecture/assembly-workshop.md) описывает шесть рабочих мест и восемь устройств: от нанесения пасты и SMD-монтажа до программирования и испытаний. Это целевая модель; текущая реализация перечислена ниже. Архитектурные решения и инструкции собраны в [каталоге документации](docs/README.md).

## Реализовано

- API `TechProcess` и `TechProcessTemplate` группы `production.factory.local/v1alpha1`;
- capability-based размещение через `LabelSelector` и node affinity;
- детерминированная revision, immutable ConfigMap параметров;
- создание и обновление `Deployment`/`DaemonSet`;
- безопасный `Recreate` по умолчанию;
- Conditions `Ready`, `Progressing`, `Degraded` и timeout rollout;
- каталог восьми устройств на шести рабочих местах и учебные рецепты A/B;
- HTTP-симуляторы операций монтажа, пайки, инспекции, программирования и измерений с управляемыми отказами;
- контракт телеметрии v1, валидация и JSON-примеры всех восьми устройств;
- периодическая генерация снимков с явным seed, общими счётчиками и готовностью с HTTP API;
- приёмник снимков в памяти, контроль порядка и свежести, Prometheus collector и обработчик `/metrics`;
- MQTT-передача через Mosquitto и отдельный процесс `telemetry-receiver` с переподключением;
- метрики оператора из status и проверка готовности обновлённой ревизии;
- unit-тесты и интеграционные тесты контроллера через `envtest`;
- Kind-конфигурация с тремя edge-worker-узлами;
- GitLab CI и контейнерные сборки оператора, симулятора и приёмника;
- профиль полного Kind-стенда с восемью MQTT-источниками (`make telemetry-demo`);
- Prometheus, правила оповещения и три готовых dashboard Grafana (`make monitoring-install`); полный E2E требует работоспособного Kind-окружения.

## Архитектурный поток

```text
UI -> Factory API -> Git -> Argo CD -> TechProcess -> Go operator -> workloads
                                                        |
                                                        +-> status and /metrics

simulator -> MQTT -> Mosquitto -> telemetry-receiver /metrics -> Prometheus -> Grafana
```

Схема показывает целевую архитектуру. Для телеметрии реализована локальная цепочка «симулятор → MQTT/Mosquitto → приёмник → `/metrics`». Prometheus и Grafana разворачиваются через `make monitoring-install`; просмотр графиков описан в [инструкции мониторинга](docs/runbooks/monitoring.md). Запуск восьми устройств в Kind описан в [инструкции стенда](docs/runbooks/telemetry-kind.md). Запуск одного устройства описан в [MQTT-runbook](docs/runbooks/telemetry-mqtt.md). Реальное оборудование и его ПО описываются теоретически согласно [ADR 0006](docs/adr/0006-simulated-telemetry.md).

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

По умолчанию используется одновузловой Kind-стенд. Полная топология с worker-узлами для SMT-монтажа, контроля/испытаний и симулируемого резерва запускается командой `make dev-up-multinode` после повышения локальных inotify-лимитов; подробности приведены в runbook.

Подробная инструкция и переключение `board-a → board-b` описаны в [`docs/runbooks/local-development.md`](./docs/runbooks/local-development.md).

Для запуска отдельных устройств и проверки операций см. [инструкцию симуляторов](docs/runbooks/equipment-simulation.md). Последовательное обновление рабочих мест и автоматический rollback ещё не реализованы.

Удаление только development-кластера:

```bash
make dev-down
```

## Основные каталоги

```text
api/v1alpha1/        Kubernetes API
internal/controller Reconcile-цикл
internal/workload   Чистая генерация workload-ресурсов
internal/equipment  Каталог устройств, учебные рецепты и результаты операций
internal/simulator  HTTP-симулятор оборудования
internal/telemetry  Контракт входных снимков и валидация
config/             CRD, RBAC, deployment и samples
infra/kind/         Локальная топология
docs/adr/           Архитектурные решения
```

## Лицензия

Apache License 2.0.
