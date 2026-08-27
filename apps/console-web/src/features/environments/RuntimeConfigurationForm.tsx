import type { RuntimeConfiguration, Variable } from "../../shared/api/types";
import { Field, SelectField, TextareaField } from "../../shared/ui/Field";

export const defaultRuntimeConfiguration = (): RuntimeConfiguration => ({
  replicas: 1,
  port: 8080,
  resources: { requests: { cpuMillis: 50, memoryMiB: 64 }, limits: { cpuMillis: 250, memoryMiB: 128 } },
  probes: { liveness: { path: "/healthz" }, readiness: { path: "/readyz" } },
  exposure: "Private",
  variables: [],
});

export function RuntimeConfigurationFields({ value, onChange, variables, onVariablesChange, variablesError, disabled = false }: { value: RuntimeConfiguration; onChange: (value: RuntimeConfiguration) => void; variables: string; onVariablesChange: (value: string) => void; variablesError?: string; disabled?: boolean }) {
  const updateResources = (group: "requests" | "limits", field: "cpuMillis" | "memoryMiB", next: number) => onChange({ ...value, resources: { ...value.resources, [group]: { ...value.resources[group], [field]: next } } });
  return <><div className="form-row"><SelectField label="Exposição" value={value.exposure} onChange={(event) => onChange({ ...value, exposure: event.target.value as "Private" | "Public", slug: event.target.value === "Private" ? undefined : value.slug })} disabled={disabled}><option value="Private">Privada</option><option value="Public">Pública</option></SelectField>{value.exposure === "Public" && <Field label="Slug público" helper={`${value.slug || "app"}.molejo.dev`} value={value.slug ?? ""} onChange={(event) => onChange({ ...value, slug: event.target.value })} pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$" maxLength={63} disabled={disabled} required/>}<Field label="Porta" type="number" min={1} max={65535} value={value.port} onChange={(event) => onChange({ ...value, port: event.target.valueAsNumber })} disabled={disabled} required/><Field label="Réplicas" type="number" min={1} max={5} value={value.replicas} onChange={(event) => onChange({ ...value, replicas: event.target.valueAsNumber })} disabled={disabled} required/></div><details className="advanced"><summary>Recursos, probes e variáveis</summary><div className="stack"><div className="form-row"><Field label="CPU solicitada (m)" type="number" min={1} max={2000} value={value.resources.requests.cpuMillis} onChange={(event) => updateResources("requests", "cpuMillis", event.target.valueAsNumber)} disabled={disabled} required/><Field label="Memória solicitada (MiB)" type="number" min={1} max={2048} value={value.resources.requests.memoryMiB} onChange={(event) => updateResources("requests", "memoryMiB", event.target.valueAsNumber)} disabled={disabled} required/><Field label="Limite de CPU (m)" type="number" min={1} max={2000} value={value.resources.limits.cpuMillis} onChange={(event) => updateResources("limits", "cpuMillis", event.target.valueAsNumber)} disabled={disabled} required/><Field label="Limite de memória (MiB)" type="number" min={1} max={2048} value={value.resources.limits.memoryMiB} onChange={(event) => updateResources("limits", "memoryMiB", event.target.valueAsNumber)} disabled={disabled} required/></div><div className="form-row"><Field label="Liveness" value={value.probes.liveness.path} onChange={(event) => onChange({ ...value, probes: { ...value.probes, liveness: { path: event.target.value } } })} pattern="^/.*" disabled={disabled} required/><Field label="Readiness" value={value.probes.readiness.path} onChange={(event) => onChange({ ...value, probes: { ...value.probes, readiness: { path: event.target.value } } })} pattern="^/.*" disabled={disabled} required/></div><TextareaField label="Variáveis não secretas" helper="Uma por linha no formato NOME=valor. Secrets terão um fluxo próprio." error={variablesError} value={variables} onChange={(event) => onVariablesChange(event.target.value)} rows={5} disabled={disabled}/></div></details></>;
}

export function parseRuntimeVariables(raw: string): { items: Variable[]; error?: string } {
  const items: Variable[] = [];
  const seen = new Set<string>();
  for (const line of raw.split("\n").map((item) => item.trim()).filter(Boolean)) {
    const separator = line.indexOf("=");
    const name = separator > 0 ? line.slice(0, separator).trim() : "";
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) return { items: [], error: `Variável inválida: ${line}` };
    if (seen.has(name)) return { items: [], error: `Variável duplicada: ${name}` };
    seen.add(name);
    items.push({ name, value: line.slice(separator + 1) });
  }
  return { items };
}

export function runtimeVariablesToText(variables: Variable[]) { return variables.map((variable) => `${variable.name}=${variable.value}`).join("\n"); }
