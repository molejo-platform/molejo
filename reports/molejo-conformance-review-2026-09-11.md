# Revisão da base de conformidade do Molejo

## 1. Resumo executivo

Revisão do checkout `8b1b0c6ea91461336066fad683b91c50691c5d90`, concentrada em `tools/cmd/molejo-conformance/`, `tools/internal/conformance/` e sua integração com o harness Kind e CI. A arquitetura constitui uma boa base de tooling OSS: perfis compilados e versionados, CLI separada, efeitos HTTP explícitos, evidência incremental, recuperação e provisionamento fora das jornadas.

O próximo investimento deve consolidar a confiabilidade do próprio testador, especialmente os caminhos de recuperação, antes de ampliar a matriz de ambientes. Não há indicação de que trocar a stack ou adotar um framework maior resolveria as lacunas encontradas.

Esta é uma revisão delimitada, não uma auditoria integral do produto. O código de produção não foi alterado e nenhum cluster foi criado ou modificado.

## 2. Stack técnica

O binário utiliza Go e a biblioteca padrão para CLI, HTTP, TLS, JSON, XML, contextos, arquivos e sinais. Os imports externos à biblioteca padrão encontrados no grafo do comando são os próprios pacotes da ferramenta. A lista extensa de dependências em `tools/go.mod` inclui ferramentas de desenvolvimento e não representa as dependências de execução do runner.

Go é adequado para um binário de automação com operações concorrentes, deadlines, TLS e distribuição simples. `flag` é suficiente para os quatro comandos atuais. Não há justificativa atual para Cobra, uma DSL YAML, plugins ou um serviço permanente apenas para executar esses perfis.

## 3. Integrações e limites

- Runner: API HTTPS do Control Plane e probes HTTPS de publicação.
- Harness: Docker, Kind, Registry local, Helm, kubectl, OpenSSL e preparação dos artefatos.
- CI: workflow dedicado com execução manual, agendada e após pushes selecionados.
- DNS público, autoridades certificadoras externas e provisionamento cloud permanecem fora dos perfis compilados.

O probe de publicação usa endereço de conexão explícito e preserva hostname/SNI. Isso comprova TLS e roteamento naquele caminho; não comprova resolução DNS pública ou toda a borda externa.

## 4. Padrões de design

Há separação útil entre catálogo, CLI, execução de cenários, cliente HTTP, relatórios, target e cleanup. JSON é o resultado canônico e JUnit deriva do mesmo estado. O provisionamento prepara um target; o perfil define a jornada do produto.

Isso se aproxima da separação deployer/tester do [kubetest2](https://github.com/kubernetes-sigs/kubetest2). A implementação do Molejo é específica, compilada e menor; não precisa reproduzir o mecanismo de extensões do projeto Kubernetes.

## 5. Simplicidade, coesão e contratos

O pacote `internal/conformance` único é apropriado para o tamanho atual. A divisão por arquivos comunica responsabilidades sem fragmentar excessivamente os tipos e helpers compartilhados.

`publication.go` tem 669 linhas e a jornada principal reúne várias transições dependentes. Isso merece acompanhamento, mas o tamanho isolado não justifica uma refatoração. Extrair etapas passa a valer a pena quando trouxer testes de decisão, diagnóstico ou reutilização concretos.

Cada perfil possui um cenário grande. As assertions dão detalhe ao JSON; JUnit conserva granularidade de jornada. Essa escolha é aceitável nesta fase, desde que não se interprete a contagem de cenários como cobertura de todas as propriedades.

O fingerprint identifica metadados do catálogo, não o conteúdo do código ou das assertions. A revisão de código precisa preservar o bump explícito da versão quando mudar a garantia, com a revisão do runner complementando a rastreabilidade.

## 6. Estrutura do projeto

- `cmd/molejo-conformance`: argumentos, sinais, credenciais em arquivo e códigos de saída.
- `catalog.go` e `model.go`: perfis, cenários e modelo de evidência.
- `runner.go`: validação, sequenciamento e finalização.
- `journey.go` e `publication.go`: comportamento do produto.
- `client.go`, `poll.go`, `target.go`: efeitos e observação.
- `report.go` e `cleanup.go`: durabilidade, veredito e recuperação.
- `tools/testing`: preparação e aceitação específica do ambiente.

Manter essas responsabilidades no repositório do produto permite revisar cenários e mudanças de contrato no mesmo diff. Um repositório de infraestrutura separado só se justifica quando houver ciclo de vida ou consumidores independentes.

## 7. Testes e validação executada

Comando executado:

```sh
GOCACHE=/tmp/molejo-go-cache go -C tools test -race -count=1 \
  -coverprofile=/tmp/molejo-conformance-review.cover \
  ./cmd/molejo-conformance/... ./internal/conformance/...
```

Resultado: ambos os pacotes passaram, sem falhas reportadas pelo race detector nessa execução. Cobertura de statements: CLI 40,5%; pacote interno 31,2%. Essa medição não inclui os testes reais executados pelo usuário em outros momentos ou um run Kind instrumentado.

Também passaram `go vet` nesses dois pacotes, `bash -n tools/testing/*.sh` e `git diff --check` antes da criação deste relatório.

A suíte tem 22 funções de teste de topo. Há casos úteis para JUnit incompleto, falha de cleanup, ledger estrangeiro, paginação opaca, mudanças de binding e seleção de endereços. `RunProfile`, as jornadas completas, verificação de target e probes TLS ficaram sem execução na cobertura desta suíte. O principal incremento recomendado é testar falhas do orquestrador com HTTP controlado, incluindo cancelamento, respostas assíncronas e falha de persistência.

Não executei o harness Kind nesta revisão. O GitHub consultado não apresentou o workflow `conformance.yml` registrado na branch padrão; a listagem recente apresentou apenas execuções anteriores de `Verify`. Portanto, há configuração local de CI, mas não foi confirmada uma execução remota deste novo runner.

## 8. Recuperação e tratamento de erros

### A1 — P2: reutilizar o diretório de saída substitui o ledger anterior

Em `tools/internal/conformance/report.go:22`, `NewReporter` aceita diretório existente e salva um relatório novo. `runner.go:46` faz isso antes da autenticação. Repetir `run` com o mesmo `--output` após interrupção pode apagar a única lista de recursos ainda pendentes, inclusive quando o novo login falha.

Reproduzido em cópia temporária isolada do pacote: registrar recurso em `old-run` e abrir outro reporter no mesmo diretório produz `new-run` com zero recursos. O teste de regressão que exige rejeitar a reutilização falha, conforme esperado para demonstrar o defeito.

Correção sugerida: rejeitar diretório com relatório existente ou criar diretório exclusivo por run ID, preservando a operação explícita de recuperação.

### A2 — P2: cleanup confunde remoção aceita com remoção concluída

Em `tools/internal/conformance/cleanup.go:40`, `Delete` usa output nulo; o cliente aceita `202`, e a entrada passa a `archived`. A retirada de AppEnvironment é assíncrona. O cleanup pode tentar remover pais antes da conclusão, e execuções posteriores ignoram a entrada marcada como arquivada.

Correção sugerida: tratar a retirada assíncrona, preservar a operação e aguardar o resultado antes de finalizar a entrada e remover dependências. Incluir casos 202 pendente, concluído e falho nos testes. O caminho normal da jornada já aguarda a operação; a lacuna está na recuperação genérica.

### A3 — P2: falha ao persistir finalização pode evitar cleanup

Em `tools/internal/conformance/runner.go:85`, erro de `FinishScenario` causa retorno direto. O cleanup dos recursos registrados não é tentado nesse caminho. É uma janela estreita nos perfis atuais, mas importante quando o filesystem falha e para futuros cenários.

Correção sugerida: centralizar a tentativa de finalização/cleanup, usando o ledger em memória, preservando o erro original e limitando o tempo da recuperação.

## 9. Observabilidade e fidelidade das provas

São pontos fortes: identidade de execução, revisão, perfil, fingerprint, target, assertions incrementais e estado de cleanup. `PENDING` não se transforma em sucesso no JUnit; a recuperação preserva o resultado histórico da execução original.

### A4 — P2: falha de consulta pode ser interpretada como ausência de filhos

Em `tools/testing/kind-conformance.sh:212`, os pipelines condicionais `kubectl | grep/jq` não distinguem erro da consulta de uma lista sem filhos. Uma consulta que falha pode deixar a prova de ausência aprovada. `set -e` não resolve o caso de comandos usados em condições.

Correção sugerida: exigir sucesso da consulta e parsing antes de avaliar ausência. Há padrão semelhante na verificação do canary.

### A5 — P2: a assertion de fence terminal não é observada pelo perfil

Em `tools/internal/conformance/publication.go:358`, o perfil registra que o fence terminal permaneceu, mas observa operação concluída, HTTP 404 e ausência de claims. A leitura direta do tombstone ocorre no harness Kind (`kind-conformance.sh:196`), e não no runner portátil.

Correção sugerida: fazer o texto do perfil refletir a garantia efetivamente observada e atribuir a prova de tombstone ao harness, ou introduzir uma observação de produto que suporte essa garantia em todos os targets. Se a garantia exigida mudar, revisar a versão do perfil.

O marker de HTTP em `publication.go:560` é constante (`molejo conformance`). Ele verifica presença de uma fixture compatível, mas não sua identidade exclusiva por execução ou release. Para ampliar a garantia de roteamento, a fixture pode devolver um identificador por run e o probe deve conferir o valor esperado.

## 10. Segurança e isolamento

O desenho inclui CA explícita, validação TLS, bloqueio de redirects para outra origem, paths relativos restritos, target verificado antes dos cenários, Workspace obrigatório em targets persistentes e recusa de substituir binding persistente. O ledger limita tipos, paths, IDs, headers e relações; ele não pretende resistir à adulteração por um operador que já possui as credenciais.

Duas limitações do harness devem ser tratadas antes de ampliar a confiança operacional: diagnósticos de Docker/Kind/componentes são copiados diretamente ao diretório de artefatos apesar da descrição de evidência sanitizada; e a coleta de diagnóstico não possui deadlines próprios antes do teardown. Não foi constatado vazamento concreto de segredo nesta revisão. A ausência de limites pode atrasar a limpeza justamente quando Docker ou cluster falham.

Não foram executadas auditoria de CVEs, análise completa de dependências ou validação de políticas de acesso do GitHub.

## 11. Acessibilidade

Não aplicável ao escopo: ferramenta CLI e harness. O Console não foi revisado.

## 12. Achados consolidados

| ID | Prioridade | Tema |
| --- | --- | --- |
| A1 | P2 | Reutilização do output apaga o ledger anterior; reproduzido localmente |
| A2 | P2 | Cleanup assíncrono finalizado antes da operação |
| A3 | P2 | Erro de persistência pode evitar tentativa de cleanup |
| A4 | P2 | Falha de leitura pode aprovar ausência no harness |
| A5 | P2 | Perfil afirma preservação de fence não observada por ele |

## 13. Próximos incrementos

1. Consolidar recuperação e evidência: A1–A4 e testes de falha do orquestrador.
2. Ajustar precisão das assertions e identidade da fixture; limitar/sanitizar diagnósticos.
3. Observar o workflow repetidamente e conservar evidência vinculada ao commit. Dez dias verdes são um critério inicial de calibração, não prova estatística de ausência de flakiness.
4. Executar os mesmos perfis em um segundo ambiente e identificar premissas específicas do Kind. O core hoje exige histórico não configurado; esse pré-requisito precisa ser explícito ao usar targets persistentes.
5. Introduzir resiliência ou desempenho como perfis com garantias próprias, sem mudar implicitamente o significado dos atuais.

## 14. Comparação OSS e avaliação

- [Kubernetes/kubetest2](https://github.com/kubernetes-sigs/kubetest2): alinhamento de separação entre ambiente e teste; Molejo ainda tem dois perfis e um harness completo mantido, sem matriz ampla comprovada.
- [Keycloak Benchmark](https://github.com/keycloak/keycloak-benchmark): alinhamento de ferramenta própria versionada no ecossistema do produto; carga, datasets e infraestrutura distribuída são áreas futuras do Molejo.
- [NGINX tests](https://github.com/nginx/nginx-tests): alinhamento de comportamento externo real, especialmente rede/TLS; aprofundar casos negativos e identidade observada é a evolução relevante.
- [NestJS CI](https://github.com/nestjs/nest/blob/master/.circleci/config.yml): referência de integrações reais e matriz de runtime; a ferramenta Molejo tem uma responsabilidade de ciclo de vida e conformidade específica, portanto comparar volume bruto não é útil.

A stack e a estrutura estão adequadas. O estágio atual é de uma ferramenta inicial de conformidade com arquitetura sólida e garantias operacionais ainda em consolidação. Para portfólio, mostrar a evolução desses limites e os experimentos reproduzíveis que os detectam é mais defensável que declarar equivalência de maturidade com projetos maiores.
