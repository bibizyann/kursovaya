# Телеметрия восьми устройств в Kind

Этап 6 собирает оператор, восемь симуляторов, Mosquitto и отдельный receiver в одном локальном Kubernetes-кластере. Prometheus и Grafana добавляются на этапе 7. Требуются Docker, Go из `go.mod`, Make и Kind (проверки рассчитаны на v0.31.0 с Kubernetes v1.35.0). `kubectl` и Kustomize устанавливаются через Makefile.

## Запуск

Из корня репозитория:

```bash
make telemetry-demo DEV_CLUSTER=factory-telemetry-dev
export KUBECONFIG="$PWD/.cache/telemetry-factory-telemetry-dev.kubeconfig"
export KUBECTL_KUBERC=false
bin/kubectl get pods -n factory-telemetry
bin/kubectl get techprocess line-one -n factory-demo -w
```

Команда создаёт кластер с capability-метками, собирает и загружает три Go-образа и `eclipse-mosquitto:2.0.22`, затем устанавливает оператор, платформу телеметрии и профиль A. Все обращения идут в именованный Kind-кластер через отдельный kubeconfig; текущий контекст пользователя не используется. Существующий кластер с этим именем используется повторно и должен иметь метки из `infra/kind/cluster-single.yaml`.

Профиль `config/overlays/telemetry` дополняет базовые A/B-шаблоны интервалом 5 секунд, MQTT-адресом, областью потока и seed 42–49. JSON patch проверяет имена компонентов перед изменением аргументов. Ревизию передаёт оператор через `FACTORY_REVISION`, имя процесса — через `FACTORY_TECH_PROCESS`. Базовый `config/samples` по-прежнему запускает симуляторы без автоматической телеметрии.

Mosquitto и receiver находятся в `factory-telemetry`, симуляторы — в `factory-demo`, оператор — в `factory-platform-system`. Компоненты платформы не имеют owner reference на `TechProcess`. Брокер и receiver работают в одном экземпляре с `Recreate`, без дискового хранилища. Перезапуск брокера теряет retained-снимки, перезапуск receiver — его оперативную память; новые снимки восстанавливают текущее наблюдение.

Это локальный профиль: MQTT анонимный, Services имеют тип ClusterIP. Порт брокера не публикуется на хосте. Для реального внедрения требуются отдельная настройка MQTT TLS/ACL и ограничение доступа к метрикам.

## Наблюдение

В отдельном терминале с тем же `KUBECONFIG`:

```bash
bin/kubectl port-forward -n factory-telemetry service/telemetry-receiver 9091:9091
```

```bash
curl --fail http://127.0.0.1:9091/healthz
curl --fail -s http://127.0.0.1:9091/metrics | grep '^factory_equipment_telemetry_fresh'
curl --fail -s http://127.0.0.1:9091/metrics | grep '^factory_equipment_info'
```

После запуска источников ожидаются восемь серий свежести со значением `1` и восемь информационных серий с `product="board-a"`. Их `revision` должна совпадать с `status.appliedRevision` процесса:

```bash
bin/kubectl get techprocess line-one -n factory-demo -o yaml
```

HTTP-пробы receiver проверяют только работу процесса. При разрыве MQTT он остаётся доступен, а свежесть данных истекает через 20 секунд. Отсутствие телеметрии не меняет readiness симуляторов или техпроцесса.

## Переключение A → B

Зафиксируйте UID pod инфраструктуры, затем измените желаемый шаблон:

```bash
bin/kubectl get pods -n factory-telemetry -o custom-columns=NAME:.metadata.name,UID:.metadata.uid
bin/kubectl patch techprocess line-one -n factory-demo --type=merge \
  -p '{"spec":{"templateRef":{"name":"board-b"},"parameters":{"product":"board-b"}}}'
bin/kubectl get techprocess line-one -n factory-demo -w
```

Дождитесь актуального `observedGeneration`, `Ready=True` и новой `appliedRevision`. Затем все восемь информационных серий должны перейти на `board-b` и новую ревизию. В процессе обновления receiver может ещё показывать свежий предыдущий снимок — это наблюдаемое, а не желаемое состояние. UID pod Mosquitto и receiver должны остаться прежними. Такой переход автоматически проверяется в E2E.

Прямой patch используется только для локального стенда. Целевой GitOps-путь остаётся через Git и Argo CD. Повторный `make telemetry-demo` снова применяет исходный процесс A.

## Изменение образов

Чтобы работающие pod получили новый код, задавайте новые теги:

```bash
make telemetry-demo DEV_CLUSTER=factory-telemetry-dev \
  DEV_OPERATOR_IMG=factory-platform:experiment-2 \
  SIMULATOR_IMG=equipment-simulator:experiment-2 \
  RECEIVER_IMG=telemetry-receiver:experiment-2
```

Переопределения применяются через временные Kustomize-профили и не изменяют отслеживаемые YAML. Повторная загрузка того же тега в Kind сама по себе не перезапускает существующие pod.

## Проверки и CI

```bash
make telemetry-render
make fmt test vet lint
make test-mqtt
KUBECONFIG="$PWD/.cache/telemetry-e2e.kubeconfig" \
  CERT_MANAGER_INSTALL_SKIP=true make test-e2e KIND_CLUSTER=factory-telemetry-e2e
```

E2E собирает и загружает три Go-образа и брокер, проверяет размещение восьми устройств, HTTP readiness, метрики каждого устройства для A/B и неизменность pod инфраструктуры. Успешный прогон удаляет свой кластер. Если проверка завершилась ошибкой, после чтения логов удалите только этот тестовый кластер:

```bash
kind export logs --name factory-telemetry-e2e .cache/telemetry-e2e-logs
kind delete cluster --name factory-telemetry-e2e
```

В `.gitlab-ci.yml` обычные jobs проверяют код, рендеринг манифестов и собирают все три бинарника. Job `telemetry-e2e` включается переменной `FACTORY_KIND_E2E=true`. Для него нужен выделенный runner с тегом `factory-kind`, локальными Docker, Go, Make и Kind, а также доступом к опубликованному API Kind. Например, выделенный shell runner с этими инструментами; его требования описаны в [документации GitLab](https://docs.gitlab.com/runner/executors/shell/). Без настроенного runner этот job не включается, и зелёный обычный pipeline не подтверждает E2E. Job не публикует образы в registry; собирает и загружает их локально.

Если kubelet не стартует с `inotify_init: too many open files`, создание кластера заблокировано лимитом окружения. Диагностика и команда изменения лимитов описаны в [локальной разработке](local-development.md). Скрипт запуска сам не меняет sysctl и не удаляет чужие кластеры.

## Завершение

Остановите port-forward через Ctrl+C. Для удаления всего созданного стенда:

```bash
kind delete cluster --name factory-telemetry-dev
```
