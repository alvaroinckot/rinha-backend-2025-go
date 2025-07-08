# Rinha de Backend 2025 - Implementação em Go + Postgres (Quick and Dirty)

Esta é uma implementação em Go para o desafio Rinha de Backend 2025.

## Tecnologias Utilizadas

- **Linguagem**: Go 1.23
- **Framework**: Gin (roteador HTTP)
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

## Funcionalidades

- **Processamento assíncrono de pagamentos** - Pagamentos são processados em goroutines em segundo plano
- **Monitoramento de verificação de saúde** - Monitora a saúde do processador de pagamentos a cada 6 segundos
- **Mecanismo de failover** - Faz fallback para o processador de backup quando o padrão falha
- **Prevenção de duplicatas** - Previne pagamentos duplicados usando ID de correlação
- **Otimizado para recursos** - Usa recursos mínimos de CPU e memória

## Endpoints

### POST /payments
Aceita solicitações de pagamento e as processa de forma assíncrona.

### GET /payments-summary
Retorna resumo dos pagamentos processados com filtragem opcional por data.

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

## Uso de Recursos

- **CPU**: 1.5 cores no total
- **Memória**: 350MB no total
- **2 instâncias da API** conforme exigido pelo desafio

