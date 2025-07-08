# Rinha de Backend 2025 - Implementação em Go + Postgres (Quick and Dirty)

Esta é uma implementação em Go para o desafio Rinha de Backend 2025.

## Tecnologias Utilizadas

- **Linguagem**: Go 1.24
- **Framework**: Gin
- **Banco de Dados**: PostgreSQL
- **Cache**: Cache em memória do Go
- **Balanceador de Carga**: Nginx
- **Containerização**: Docker

## Arquitetura

A solução consiste em:

1. **3 instâncias da API** - Aplicações Go usando o framework Gin
2. **Banco de dados PostgreSQL** - Para armazenar registros de pagamentos
3. **Balanceador de carga Nginx** - Distribui requisições entre as instâncias da API
4. **Cache em memória** - Para cache dos resultados de verificação de saúde

## Executando Localmente

1. Inicie os processadores de pagamento primeiro:
```bash
# No diretório payment-processor
docker-compose up -d
```

2. Inicie esta aplicação:
```bash
docker-compose up -d
```

A API estará disponível em `http://localhost:9999`

