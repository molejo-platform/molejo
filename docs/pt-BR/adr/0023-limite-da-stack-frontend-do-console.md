# ADR 0023: Limite da stack frontend do Console

## Status

Aceita para a arquitetura alpha.

## Contexto

O Console já possui tokens semânticos da Molejo, cascade layers em CSS nativo,
estilos pertencentes às features e contratos React. A pressão de manutenção está
nos comportamentos interativos complexos, formulários e arquivos de rota grandes,
e não na ausência de classes utilitárias. Substituir o sistema visual criaria uma
migração extensa sem resolver foco, acessibilidade ou ownership de domínio.

## Decisão

O Console mantém React, TypeScript, Vite, TanStack Router, TanStack Query,
TanStack Virtual, CSS nativo, custom properties semânticas e cascade layers. CSS
Modules isolam estilos privados; contratos visuais compartilhados permanecem
globais.

Base UI pode fornecer comportamento acessível complexo somente por adapters em
`shared/ui`. Ícones Lucide também são expostos somente pelo contrato local
`Icon`. HTML nativo continua sendo o padrão. React Hook Form e Zod ficam
restritos a workflows multi-etapa ou estruturalmente complexos e não substituem a
validação de negócio da API.

Tailwind CSS, shadcn/ui, preprocessadores, CSS-in-JS, providers de tema em runtime
e bibliotecas globais de estado de cliente não fazem parte da stack atual.
Bibliotecas de tabelas, gráficos ou workbench de componentes entram somente com
um caso de uso comprovado.

## Consequências

- A Molejo mantém uma única fonte de tokens semânticos e identidade própria.
- Bibliotecas externas permanecem substituíveis atrás de contratos locais.
- Features não podem importar Base UI ou Lucide diretamente.
- Arquivos de produção autorados permanecem com no máximo 400 linhas.
- Nova dependência exige caso concreto, medição de bundle, cobertura acessível e
  caminho de saída.
- Tailwind ou shadcn só podem ser reconsiderados por uma migração que remova, em
  vez de duplicar permanentemente, o contrato atual.

## Alternativas consideradas

Tailwind com shadcn foi rejeitado neste momento porque moveria estilos existentes
para outra sintaxe e tornaria o código gerado responsabilidade local. Sass e
outros preprocessadores foram rejeitados porque o CSS atual já oferece os
recursos necessários. Primitives modais escritos manualmente foram rejeitados
porque foco, teclado, fundo inerte e bloqueio de scroll são comportamentos
especializados.

## Referências

- [`apps/console-web/ARCHITECTURE.md`](../../../apps/console-web/ARCHITECTURE.md)
- [Base UI](https://base-ui.com/react/overview/about)
- [Recursos CSS do Vite](https://vite.dev/guide/features)
- [shadcn/ui](https://ui.shadcn.com/docs)
- [Tailwind CSS v4](https://tailwindcss.com/blog/tailwindcss-v4)
