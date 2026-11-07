# Factory UI: обзор участка

U1 добавляет панель с шестью рабочими местами и восемью устройствами: профиль, готовность ревизии, размещение pod, наблюдаемый рецепт, показания и ссылки Grafana. Выбор профиля, редактирование параметров и управление симуляторами из UI пока недоступны.

## На существующем стенде

Для `factory-platform-dev`, созданного по [инструкции мониторинга](monitoring.md):

```bash
make ui-install
make ui-open
```

Оставьте port-forward работающим и откройте **http://localhost:8081**. Доступ ограничен loopback; отдельного логина в локальном read-only профиле нет. Публикация порта в общую сеть требует отдельного профиля аутентификации и TLS.

`ui-install` собирает бинарник и контейнер, загружает образ в Kind, применяет `config/ui` и ждёт Deployment. По умолчанию тег вычисляется по содержимому бинарника, включая assets: изменение UI запускает новый pod. Для собственного тега задайте уникальный `UI_IMG=factory-api:<tag>`; digest-ссылки installer не поддерживает.

Для другого кластера передавайте одно имя во все команды:

```bash
make ui-install DEV_CLUSTER=factory-monitoring-dev
make ui-open DEV_CLUSTER=factory-monitoring-dev
```

Используется `.cache/telemetry-<имя>.kubeconfig`, а не текущий контекст. Namespace `factory-demo` уже должен существовать. Установка UI не меняет техпроцесс, оператор и мониторинг.

## С нуля

Нужны Docker, Kind, Go и GNU Make. `kubectl` и `kustomize` устанавливаются Make targets.

```bash
make telemetry-demo DEV_CLUSTER=factory-ui-dev
make monitoring-install DEV_CLUSTER=factory-ui-dev
make ui-install DEV_CLUSTER=factory-ui-dev
make ui-open DEV_CLUSTER=factory-ui-dev
```

Для графиков в другом терминале:

```bash
make grafana-password DEV_CLUSTER=factory-ui-dev
make grafana-open DEV_CLUSTER=factory-ui-dev
```

Ссылки открывают `http://localhost:3000` с фильтрами процесса/устройства. Авторизация Grafana остаётся собственной, пароль Factory API не получает. Без её port-forward обзор продолжает работать, но ссылки на графики недоступны.

## Интерпретация состояний

- «Профиль в кластере» — текущий `TechProcess.spec.templateRef`. GitOps появится в U2.
- Готовность относится к целевой ревизии. Старый status, другая версия шаблона и смешанный rollout не подтверждают новый Ready.
- Свежесть телеметрии независима от Ready. Данные другой ревизии явно отмечаются; они не подтверждают новый рецепт.
- «Устарела» — истёк срок свежести снимка. «Нет данных» — нет доступного наблюдения. «Неизвестно» — источник недоступен или данные неоднозначны/неполны. Показания не заменяются нулём.
- Свежая неисправность отличается от отсутствия данных. Подготовка и калибровка доступны в подробной карточке.
- Рабочее место фильтрует устройства; шестое место содержит три прибора. Поиск работает по ID, типу и модели. Фильтр готовых приложений использует Kubernetes readiness, а не MQTT.
- Карточка показывает встроенный желаемый рецепт, наблюдаемую конфигурацию, pod/node и показания. Нет подтверждённых pod — не значит, что существует свободное физическое оборудование.

Опрос повторяется через 3 секунды после ответа. При потере API текущие статусы сбрасываются в Unknown. Маршрут не моделирует движение плат; показания синтетические.

## API и конфигурация

| Endpoint | Назначение |
|---|---|
| `GET /` | Обзор участка |
| `GET /api/v1/overview` | JSON schemaVersion 1: источники, процесс, рабочие места, устройства |
| `GET /healthz` | HTTP-процесс жив; внешние источники не влияют |

Overview возвращает 200 и при частичной недоступности: проверяйте `kubernetes.available`, `prometheus.available`, `receiver.available` и состояния. Запись не поддерживается. Query-параметры не позволяют выбрать другой namespace/process или выполнить произвольный PromQL.

| Flag | По умолчанию |
|---|---|
| `--listen` | `127.0.0.1:8081`; в контейнере явно `:8081` |
| `--kubeconfig` | Пусто: service account; локально нужен явный файл |
| `--namespace` / `--process` | `factory-demo` / `line-one` |
| `--prometheus-url` | `http://factory-monitoring-prometheus.factory-monitoring.svc:9090` |
| `--grafana-url` | `http://localhost:3000`; пустое значение скрывает ссылки |
| `--source-timeout` | `2s`; больше нуля и не больше `10s` |
| `--freshness-timeout` | `20s`; согласовать с receiver |
| `--max-sample-age` | `15s` при scrape interval `5s` |

Для другого namespace согласованно измените flags и Role/RoleBinding в overlay. Поддерживается одна линия на экземпляр API и один receiver target. Прав на Secret, node, exec и запись нет.

Локальная разработка бинарника:

```bash
make build-factory-api
make prometheus-open DEV_CLUSTER=factory-platform-dev
```

В другом терминале:

```bash
./bin/factory-api \
  --kubeconfig="$PWD/.cache/telemetry-factory-platform-dev.kubeconfig" \
  --prometheus-url=http://127.0.0.1:9090
```

Используется identity указанного kubeconfig. Минимальный RBAC проверяйте контейнерным профилем. Если 8081 занят `ui-open`, остановите port-forward или задайте другой `--listen`.

## Проверки

```bash
make test-unit
make fmt test vet lint
make ui-render
```

Отдельный Kind E2E проверяет A/B, карточки, read-only права и Unknown при остановке receiver без потери Ready:

```bash
KUBECONFIG="$PWD/.cache/ui-e2e.kubeconfig" \
  CERT_MANAGER_INSTALL_SKIP=true FACTORY_MONITORING_E2E=true FACTORY_UI_E2E=true \
  make test-e2e KIND_CLUSTER=factory-ui-e2e
```

Не указывайте development-кластер как `KIND_CLUSTER`: E2E изменяет тестовые ресурсы и после успеха удаляет кластер. Ошибка подготовки узла означает, что проверки приложения ещё не запускались.

Опциональная проверка браузера использует Python 3, установленный Chromium/Chrome/Brave и закреплённый клиент DevTools. Она не меняет процесс или мониторинг:

```bash
python3 -m venv .cache/ui-browser-venv
.cache/ui-browser-venv/bin/pip install -r test/ui/requirements.txt
.cache/ui-browser-venv/bin/python test/ui/smoke.py \
  --browser=/path/to/chromium \
  --url=http://127.0.0.1:8081 --expect-fresh
```

Укажите фактический бинарник, например `/opt/brave.com/brave/brave`. Без `--expect-fresh` допускается отсутствие мониторинга. Проверяются поиск, фильтры, карточка, Escape, ссылки, мобильная ширина и потеря/восстановление доступа браузера к API. Скриншоты и журнал сохраняются в `.cache/ui-browser/`. Временный профиль браузера удаляется после проверки.

## Диагностика и удаление

```bash
export KUBECONFIG="$PWD/.cache/telemetry-factory-platform-dev.kubeconfig"
bin/kubectl get pods -n factory-ui
bin/kubectl logs deployment/factory-api -n factory-ui
curl -fsS http://localhost:8081/api/v1/overview
```

При недоступном Kubernetes проверьте RoleBinding и namespace. При недоступном Prometheus — Service/DNS и установку мониторинга. Для receiver проверьте target `factory-telemetry`: старое `up=1` не считается бесконечно актуальным; текущий профиль допускает ровно один target.

Ctrl+C завершает только port-forward. Удаление UI с явно установленным выше kubeconfig:

```bash
bin/kubectl delete -k config/ui
```

Удаляются namespace UI, его workload и read RBAC в `factory-demo`; техпроцесс и мониторинг сохраняются.
