# ADR 0017: limite da identidade humana

## Status

Aceito para a arquitetura alpha.

## Decisão

A Molejo separa a conta humana durável dos métodos de autenticação. Um `user`
possui perfil, estado de ciclo de vida, papéis da instalação, associações a
Workspaces e histórico de auditoria. Credenciais locais de senha, MFA e futuras
identidades externas autenticam esse usuário, mas não são proprietárias de sua
autorização.

Um futuro suporte a OIDC ou SAML deve mapear o par imutável `(issuer, subject)`
do provedor para um usuário Molejo. E-mail, nome de exibição e username são
atributos mutáveis e não podem identificar a identidade externa. Grupos do
provedor devem ser traduzidos explicitamente para papéis ou relações Molejo;
eles não contornam a política de autorização.

No alpha, convites criam o usuário no estado `Invited`, sem senha. Aceitar um
convite expirável e de uso único cria a credencial local e ativa o mesmo
usuário. Alterações de credencial invalidam sessões pela versão de autenticação
do usuário.

## Consequências

Workspaces e registros de auditoria permanecem estáveis quando o provedor de
autenticação muda. Adicionar um provedor externo exige um adaptador e um
armazenamento de mapeamento, não mudanças nos contratos de associação ou do
domínio de aplicações. Vinculação de contas e provisionamento pelo provedor
continuam decisões futuras explícitas.

## Alternativas consideradas

Usar username ou e-mail como identidade do provedor foi rejeitado porque ambos
podem mudar ou colidir. Manter autorização apenas no provedor foi rejeitado por
acoplar a política de aplicações da Molejo a um único provedor.

## Referências

- [ADR 0015: ownership de capacidades](0015-ownership-de-capacidades.md)
- [Plano da fundação de gestão de usuários](../../plans/2026-09-06-user-management-foundation/MANIFESTO.md)
