# Prometheus и Grafana: запуск и просмотр графиков

Этап 7 добавляет хранение метрик и три готовых dashboard. Поток данных:

```text
8 симуляторов → MQTT → Mosquitto → receiver /metrics ─┐
                                                   ├→ Prometheus → Grafana
                           оператор /metrics ──────┘
```

Симуляторы отправляют снимки раз в 5 секунд. Prometheus опрашивает receiver и оператор каждые 5 секунд и вычисляет правила с тем же интервалом. Grafana читает историю из Prometheus. Ни один из этих компонентов не управляет готовностью оборудования через метрики.

## Если стенд уже настроен

Для стандартного кластера `factory-platform-dev` из корня проекта:

```bash
make grafana-password
make grafana-open
```

Первая команда выводит пароль. Вторая держит port-forward; оставьте терминал работающим и откройте **http://localhost:3000**. Логин — **admin**. Пароль генерируется при первой установке и хранится в Kubernetes Secret, в Git его нет.

В Grafana откройте **Dashboards → Factory**. Все три dashboard и источник данных **Prometheus** настроены автоматически. Импорт JSON и ручное добавление источника не нужны. Прямые ссылки при работающем port-forward:

- [Factory / Equipment](http://localhost:3000/d/factory-equipment) — состояние оборудования;
- [Factory / Readings](http://localhost:3000/d/factory-readings) — показания приборов;
- [Factory / Tech processes](http://localhost:3000/d/factory-processes) — работа оператора.

Если использовано другое имя Kind-кластера, передавайте `DEV_CLUSTER=<имя>` во все Make-команды. Они используют `.cache/telemetry-<имя>.kubeconfig`, а не текущий контекст `kubectl`.

## Установка с нуля

Нужны Docker, Kind, Go из `go.mod`, Make, curl, tar и OpenSSL. Закреплённый Helm скачивается в `bin/` автоматически с проверкой SHA-256.

```bash
make telemetry-demo DEV_CLUSTER=factory-monitoring-dev
make monitoring-install DEV_CLUSTER=factory-monitoring-dev
make grafana-password DEV_CLUSTER=factory-monitoring-dev
make grafana-open DEV_CLUSTER=factory-monitoring-dev
```

`telemetry-demo` создаёт полный профиль A с восемью устройствами. `monitoring-install` устанавливает отдельный Helm release `factory-monitoring` в namespace `factory-monitoring`, не изменяя TechProcess. Используется `kube-prometheus-stack` версии **92.22.0**, закреплённой в `config/monitoring/versions.env`; параметры находятся в `values.yaml`. Установка ждёт готовности компонентов. Повторная установка сохраняет пароль и применяет изменения dashboard и правил.

Для обновления уже запущенных Go-бинарников используйте новые теги, иначе работающие pod могут продолжить использовать старый образ:

```bash
make telemetry-demo DEV_CLUSTER=factory-platform-dev \
  DEV_OPERATOR_IMG=factory-platform:monitoring-stage7 \
  SIMULATOR_IMG=equipment-simulator:monitoring-stage7 \
  RECEIVER_IMG=telemetry-receiver:monitoring-stage7
make monitoring-install DEV_CLUSTER=factory-platform-dev
```

Старый кластер должен иметь capability-метки текущего профиля. Если pod стоят в Pending, проверьте события и метки по [инструкции Kind](telemetry-kind.md). Для одновузлового учебного стенда с ролью `all-in-one-development` можно явно привести его симулируемые capabilities к текущему профилю:

```bash
export KUBECONFIG="$PWD/.cache/telemetry-factory-platform-dev.kubeconfig"
bin/kubectl label nodes -l factory.local/workstation=all-in-one-development \
  factory.local/paste-printing=true factory.local/component-placement=true \
  factory.local/reflow-soldering=true factory.local/optical-inspection=true \
  factory.local/manual-assembly=true factory.local/programming-test=true --overwrite
```

Эта команда предназначена только для симуляционного Kind-профиля: она не подтверждает наличие физического оборудования на реальном узле.

## Как читать dashboard

Выберите сверху namespace `factory-demo` и процесс `line-one`. В первых двух dashboard также доступны рабочее место и устройство. `All` показывает все значения. Справа сверху задайте **Last 15 minutes** или **Last 30 minutes**; автообновление — **5s**. Сразу после запуска история короткая, а графикам скорости операций нужно несколько опросов.

| Dashboard | Что показывает |
|---|---|
| Equipment | Доступность receiver, свежесть каждого потока, состояние устройств во времени, калибровку, операции/ошибки/отклонения в минуту, наблюдаемую конфигурацию и активные alerts |
| Readings | Температуру печи и уставку; напряжения БП и мультиметра с границами; ток и лимит; длительность последней операции; состояния выхода БП и готовности температуры |
| Tech processes | Доступность метрик оператора, Ready, актуальность status, желаемое и готовое число экземпляров, длительность завершённого rollout, текущую ревизию, Conditions и ошибки Reconcile |

Показания доступны только у соответствующих устройств: например, у программатора нет температуры печи. В таком случае пустая панель ожидаема. Длительность последней операции отсутствует до первой завершённой операции.

В Equipment состояния различаются:

- **Healthy** — свежий снимок сообщает об исправности;
- **Fault** — свежий снимок сообщает о неисправности;
- **Unknown / Stale / missing** — снимок отсутствует или старше 20 секунд;
- **Receiver Unavailable** — Prometheus не может опросить receiver. Показания скрываются, на графиках остаётся разрыв.

Разрыв линии означает отсутствие измерения, а не ноль. Нулевой ток или напряжение, если они действительно переданы свежим снимком, остаются нулём. Информационная таблица показывает наблюдаемую ревизию: при A → B до нового снимка может быть видна предыдущая конфигурация.

Скорость операций рассчитывается через `rate` накопительных счётчиков за минутное окно. Это синтетические операции отдельных устройств, не выпуск плат и не OEE. После обрыва связи история отдельных операций не восстанавливается; скорость в окне может усреднить прирост счётчика за разрыв. Длительность rollout — сохранённая разница `completedAt - startedAt`, а не гистограмма; краткие rollout между опросами могут не попасть в историю.

## Prometheus и собственные запросы

В другом терминале:

```bash
make prometheus-open
```

Откройте **http://localhost:9090**. На странице **Targets** должны быть `factory-operator` и `factory-telemetry` в состоянии **UP**. В интерфейсе запросов можно выполнить:

```promql
count(factory:equipment_fresh == 1)
```

Ожидаемое значение для полного стенда — `8`.

```promql
factory_temperature_celsius{equipment_id="reflow-oven-01"}
factory_techprocess_ready_workloads{tech_process="line-one"}
factory_techprocess_rollout_duration_seconds{tech_process="line-one"}
```

В Grafana те же запросы можно выполнять через **Explore → Prometheus**. Для физического графика с корректной обработкой недоступности используйте запросы готового dashboard: они дополнительно проверяют свежесть и доступность receiver. Сырые метрики и рассчитанные recording rules не обязаны обновляться в одну и ту же миллисекунду.

## Alerts

Правила отображаются на странице **Alerts** Prometheus и в таблице **Active factory alerts** dashboard Equipment. Настроены недоступность двух источников, устаревшая телеметрия, свежая неисправность, отсутствие калибровки, Degraded и устаревший status процесса. Обычно условие должно сохраняться 30 секунд; для свежей неисправности — 15 секунд. До этого alert находится в Pending.

Alertmanager, отправка писем и внешние уведомления в этом профиле отключены. Alerts служат наблюдению и не останавливают workloads. Пустая таблица alerts означает отсутствие активных правил только при работающем Prometheus; проверяйте также доступность источников.

## Хранение и ограничения

Prometheus использует PVC 2 GiB класса `standard`, retention 7 дней и лимит исторических блоков 1 GB. WAL и служебные файлы занимают дополнительное место. Это локальный диск Kind: перезапуск pod сохраняет PVC, удаление кластера уничтожает историю. Резервное копирование этим профилем не предусмотрено.

Grafana использует временное хранилище. Источник данных, пароль и три dashboard восстанавливаются из конфигурации и Secret. Изменения dashboard через UI не сохраняются: редактируйте JSON в `config/monitoring/dashboards` и выполняйте `make monitoring-install`. Произвольные пользовательские настройки Grafana могут потеряться после пересоздания pod.

Services внутренние, для браузера используется port-forward. Метрики оператора доступны Prometheus по HTTPS с service-account token и отдельным RBAC. Проверка его самоподписанного сертификата отключена только в локальном профиле; для реального развёртывания требуется доверенный сертификат. Приёмник опрашивается по HTTP внутри стенда.

## Проверки и диагностика

```bash
make monitoring-render
make test-monitoring
make fmt test vet lint
```

`test-monitoring` требует Docker: проверяет правила через promtool, сценарии отсутствия/устаревания данных и сброса счётчика, затем выполняет каждый запрос dashboard через временный Prometheus. Контейнер удаляется после проверки. Полный E2E с мониторингом включается отдельно:

```bash
KUBECONFIG="$PWD/.cache/monitoring-e2e.kubeconfig" \
  CERT_MANAGER_INSTALL_SKIP=true FACTORY_MONITORING_E2E=true \
  make test-e2e KIND_CLUSTER=factory-monitoring-e2e
```

В GitLab обычная проверка рендерит chart; выделенный job `telemetry-e2e`, включённый через `FACTORY_KIND_E2E=true`, также проверяет правила и запускает полный сценарий с Prometheus/Grafana.

Если графики пустые:

1. Проверьте выбранные фильтры и временной диапазон.
2. Проверьте Targets: UP подтверждает scrape, но не наличие свежей телеметрии.
3. Проверьте `factory:equipment_fresh` и pod симуляторов/receiver.
4. Если нет метрик `factory_techprocess_*`, убедитесь, что запущен актуальный образ оператора, а не старый pod с переиспользованным тегом.
5. Если Prometheus Pending — проверьте PVC и StorageClass; этот профиль использует Kind `standard`.

```bash
export KUBECONFIG="$PWD/.cache/telemetry-factory-platform-dev.kubeconfig"
bin/kubectl get pods,pvc -n factory-monitoring
bin/kubectl get pods -n factory-telemetry
bin/kubectl get pods -n factory-demo
bin/kubectl get techprocess line-one -n factory-demo -o yaml
```

Остановка port-forward через Ctrl+C закрывает только доступ из браузера. Сбор метрик продолжает работать внутри кластера.
