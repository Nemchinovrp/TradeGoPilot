# TradeGoPilot

Go-клиент Invest API через gRPC: TLS, авторизация, таймауты и получение счетов.

## Запуск

Нужен Go 1.25.3 или новее и токен Invest API выбранного контура.

```sh
cp .env.example .env
# Заполните INVEST_TOKEN в .env локально.
set -a
source .env
set +a
go run ./cmd/tradegopilot
```

Программа читает окружение; файл .env автоматически не загружается.
Если локальный GOROOT указывает на другую версию Go, запускайте
`env -u GOROOT go run ./cmd/tradegopilot` (аналогично для проверок).
Ответ выводится JSON в stdout, tracking-id и ошибки — в stderr.
Пустой список счетов песочницы допустим: программа счета не создаёт.

| Переменная | По умолчанию | Назначение |
| --- | --- | --- |
| INVEST_TOKEN | — | Обязательный токен без префикса Bearer |
| INVEST_ENV | sandbox | sandbox или production |
| INVEST_APP_NAME | roman.TradeGoPilot | Заголовок x-app-name |
| INVEST_TIMEOUT | 10s | Положительный таймаут RPC, включая соединение |

Для реальных счетов задайте INVEST_ENV=production и соответствующий токен.
Для этой команды достаточно доступа только на чтение.
Токен храните в локальном .env, который исключён из Git.

## Структура

- internal/invest — конфигурация и переиспользуемый клиент.
- cmd/tradegopilot — получение счетов; Ctrl+C отменяет запрос.

Protobuf-контракты: github.com/tinkoff/invest-api-go-sdk v1.4.6.
SDK архивирован; используется только пакет proto, соединение управляется
напрямую через gRPC. Для новых методов потребуется обновление контрактов.

## Проверки

```sh
go test ./...
go vet ./...
go build ./...
```

Документация: https://tinkoff.github.io/investAPI/grpc/
