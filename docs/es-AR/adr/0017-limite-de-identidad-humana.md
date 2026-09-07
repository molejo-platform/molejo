# ADR 0017: límite de identidad humana

## Estado

Aceptado para la arquitectura alpha.

## Decisión

Molejo separa la cuenta humana durable de los métodos de autenticación. Un
`user` posee su perfil, estado de ciclo de vida, roles de instalación,
membresías de Workspace e historial de auditoría. Las credenciales locales de
contraseña, MFA y futuras identidades externas autentican ese usuario, pero no
son propietarias de su autorización.

El soporte futuro de OIDC o SAML debe mapear el par inmutable `(issuer,
subject)` del proveedor a un usuario Molejo. Correo, nombre visible y username
son atributos mutables y no deben identificar la identidad externa. Los grupos
del proveedor deben traducirse explícitamente a roles o relaciones Molejo; no
eluden la política de autorización.

En el alpha, las invitaciones crean el usuario en estado `Invited`, sin
contraseña. Aceptar una invitación expirable y de uso único crea la credencial
local y activa el mismo usuario. Los cambios de credencial invalidan sesiones
mediante la versión de autenticación del usuario.

## Consecuencias

Los Workspaces y registros de auditoría permanecen estables si cambia el
proveedor de autenticación. Agregar un proveedor externo requiere un adaptador
y un almacenamiento de mapeo, no cambios en contratos de membresía o del
dominio de aplicaciones. La vinculación de cuentas y el aprovisionamiento por
el proveedor siguen siendo decisiones futuras explícitas.

## Alternativas consideradas

Se rechazó usar username o correo como identidad del proveedor porque pueden
cambiar o colisionar. También se rechazó mantener la autorización solamente en
el proveedor porque acoplaría la política de aplicaciones de Molejo a un único
proveedor.

## Referencias

- [ADR 0015: ownership de capacidades](0015-ownership-de-capacidades.md)
- [Plan de la base de gestión de usuarios](../../plans/2026-09-06-user-management-foundation/MANIFESTO.md)
