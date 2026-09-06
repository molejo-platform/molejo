# Contribuição

As diretrizes de contribuição serão adicionadas em breve.

## Testes

Execute a suíte rápida sem serviços externos:

```bash
just test
```

Os testes de integração com PostgreSQL exigem o Docker em execução. O
Testcontainers inicia o PostgreSQL 17.6 em uma porta aleatória do host e o
remove depois da suíte de cada pacote:

```bash
just integration-test
```

Para usar um PostgreSQL existente em vez do Docker, informe sua URL:

```bash
MOLEJO_TEST_DATABASE_URL='postgres://user:password@host/database?sslmode=disable' just integration-test
```

`just verify` executa ambas as suítes e todos os gates de qualidade do
repositório.
