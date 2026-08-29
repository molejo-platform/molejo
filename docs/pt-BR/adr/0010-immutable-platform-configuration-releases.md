# ADR-0010: Immutable Platform Configuration Releases

Status: Draft

## Context

Processos da plataforma leem configuração e credenciais na inicialização.
Atualizar ConfigMap ou Secret com nome fixo não altera o template do Pod, e
restarts imperativos tornam aplicações idênticas não idempotentes e escondem a
versão ativa.

## Decision

Configuração não secreta da plataforma é renderizada em ConfigMaps imutáveis e
endereçados por conteúdo, separados por consumidor. Credenciais rotacionáveis
permanecem fora do Git e são copiadas para versões imutáveis de Secret; releases
renderizados referenciam nomes exatos. Certificados gerenciados podem projetar um
fingerprint não secreto no template do Pod quando o controller exigir nome fixo.

Cada release é renderizado a partir de checkout limpo e commitado e registra o
commit de origem. Um release sem mudanças preserva os templates. O garbage
collection roda somente após o aceite, mantém as duas versões mais novas e nunca
remove uma versão ainda referenciada por Deployment, StatefulSet, DaemonSet ou Job.

## Consequences

Mudanças de configuração reiniciam somente seus consumidores, rollback pode
selecionar versão anterior e rotação não divide silenciosamente consumidores entre
valores novos e antigos. Material secreto não entra no Git nem nos metadados de
release. O laboratório ainda usa uma réplica e não declara rotação sem interrupção
nem HA.
