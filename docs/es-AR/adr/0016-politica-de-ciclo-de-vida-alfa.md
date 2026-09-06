# ADR 0016: Política de ciclo de vida alfa

## Estado

Aceptado para la arquitectura alfa.

## Decisión

Las releases alfa no ofrecen compatibilidad retroactiva de comandos de la CLI,
schemas de API, conversión de CRDs ni upgrades in-place. Una release puede reemplazar
un contrato experimental para mejorar su límite de largo plazo. La transición
soportada entre instalaciones alfa diferentes es backup cuando corresponda,
teardown y reinstalación limpia.

## Consecuencias

El proyecto puede validar contratos duraderos antes de convertir la compatibilidad
en una promesa pública. Los instaladores fallan con una orientación de reinstalación
limpia cuando encuentran otro alfa. Los controles de ownership y seguridad de
secretos siguen siendo obligatorios aunque no haya compatibilidad.
