# ADR 0023: Límite de la stack frontend de la Consola

## Estado

Aceptada para la arquitectura alfa.

## Contexto

La Consola ya tiene tokens semánticos de Molejo, cascade layers en CSS nativo,
estilos pertenecientes a cada feature y contratos React. La presión de
mantenimiento está en el comportamiento interactivo complejo, los formularios y
los archivos de ruta grandes, no en la ausencia de clases utilitarias. Sustituir
el sistema visual crearía una migración extensa sin resolver foco, accesibilidad
u ownership de dominio.

## Decisión

La Consola mantiene React, TypeScript, Vite, TanStack Router, TanStack Query,
TanStack Virtual, CSS nativo, custom properties semánticas y cascade layers. CSS
Modules aísla estilos privados; los contratos visuales compartidos siguen siendo
globales.

Base UI puede aportar comportamiento accesible complejo solamente mediante
adapters en `shared/ui`. Los iconos Lucide también se exponen solamente mediante
el contrato local `Icon`. HTML nativo sigue siendo el estándar. React Hook Form y
Zod se limitan a workflows multi-etapa o estructuralmente complejos y no
reemplazan la validación de negocio de la API.

Tailwind CSS, shadcn/ui, preprocesadores, CSS-in-JS, providers de tema en runtime
y librerías globales de estado de cliente no forman parte de la stack actual.
Librerías de tablas, gráficos o workbench de componentes entran solamente con un
caso de uso comprobado.

## Consecuencias

- Molejo mantiene una única fuente de tokens semánticos y una identidad propia.
- Las librerías externas siguen siendo sustituibles detrás de contratos locales.
- Las features no pueden importar Base UI o Lucide directamente.
- Los archivos de producción autorados permanecen con un máximo de 400 líneas.
- Una nueva dependencia requiere un caso concreto, medición de bundle, cobertura
  accesible y un camino de salida.
- Tailwind o shadcn solo pueden reconsiderarse mediante una migración que elimine,
  en lugar de duplicar permanentemente, el contrato actual.

## Alternativas consideradas

Tailwind con shadcn fue rechazado por ahora porque movería estilos existentes a
otra sintaxis y haría del código generado una responsabilidad local. Sass y otros
preprocesadores fueron rechazados porque el CSS actual ya ofrece los recursos
necesarios. Los primitives modales escritos manualmente fueron rechazados porque
foco, teclado, fondo inerte y bloqueo de scroll son comportamientos especializados.

## Referencias

- [`apps/console-web/ARCHITECTURE.md`](../../../apps/console-web/ARCHITECTURE.md)
- [Base UI](https://base-ui.com/react/overview/about)
- [Recursos CSS de Vite](https://vite.dev/guide/features)
- [shadcn/ui](https://ui.shadcn.com/docs)
- [Tailwind CSS v4](https://tailwindcss.com/blog/tailwindcss-v4)
