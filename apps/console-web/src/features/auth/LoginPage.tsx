import { FormEvent, useState } from "react";
import { useNavigate } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import { useLoginMutation } from "./model";

export function LoginPage() {
  const navigate = useNavigate();
  const mutation = useLoginMutation();
  const [actor, setActor] = useState<"owner" | "tester-1" | "tester-2">("owner");
  const [password, setPassword] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      await mutation.mutateAsync({ actor, password });
      setPassword("");
      await navigate({ to: "/deployments", replace: true });
    } catch {
      // The mutation error is rendered below without exposing server details.
    }
  }

  return (
    <main className="shell narrow">
      <div className="brand"><span className="mark">F</span><span>Fruto Console</span></div>
      <section className="card">
        <p className="eyebrow">Laboratório privado</p>
        <h1>Entre para gerenciar seu Workspace</h1>
        <form onSubmit={submit} className="stack">
          <SelectField label="Identidade" value={actor} onChange={(event) => setActor(event.target.value as typeof actor)}>
            <option value="owner">owner</option>
            <option value="tester-1">tester-1</option>
            <option value="tester-2">tester-2</option>
          </SelectField>
          <Field label="Senha" type="password" value={password} onChange={(event) => setPassword(event.target.value)} required autoComplete="current-password" />
          {mutation.isError && <Alert>{userFacingError(mutation.error)}</Alert>}
          <Button type="submit" disabled={mutation.isPending}>{mutation.isPending ? "Entrando…" : "Entrar"}</Button>
        </form>
      </section>
    </main>
  );
}
