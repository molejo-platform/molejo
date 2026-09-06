# ADR 0016: Política de ciclo de vida alpha

## Status

Aceito para a arquitetura alpha.

## Decisão

Releases alpha não oferecem compatibilidade retroativa de caminhos da CLI,
schemas de API, conversão de CRDs ou upgrades in-place. Uma release pode substituir
um contrato experimental para melhorar seu limite de longo prazo. A transição
suportada entre instalações alpha diferentes é backup quando relevante, teardown e
reinstalação limpa.

## Consequências

O projeto pode validar contratos duráveis antes de tornar compatibilidade uma
promessa pública. Instaladores falham com orientação de reinstalação limpa quando
encontram outro alpha. Verificações de ownership e segurança de segredos continuam
obrigatórias mesmo sem compatibilidade.
