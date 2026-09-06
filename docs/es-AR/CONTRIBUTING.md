# Contribuciones

Las pautas de contribución se agregarán próximamente.

## Pruebas

Ejecutá la suite rápida sin servicios externos:

```bash
just test
```

Las pruebas de integración con PostgreSQL requieren Docker en ejecución.
Testcontainers inicia PostgreSQL 17.6 en un puerto aleatorio del host y lo
elimina después de la suite de cada paquete:

```bash
just integration-test
```

Para usar una instancia PostgreSQL existente en lugar de Docker, proporcioná
su URL:

```bash
MOLEJO_TEST_DATABASE_URL='postgres://user:password@host/database?sslmode=disable' just integration-test
```

`just verify` ejecuta ambas suites y todos los controles de calidad del
repositorio.
