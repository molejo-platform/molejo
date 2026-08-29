import { FormEvent, useState } from "react";
import { useNavigate } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field } from "../../shared/ui/Field";
import { useCompleteTOTPLoginMutation, useLoginMutation } from "./model";

export function LoginPage() {
  const navigate = useNavigate();
  const mutation = useLoginMutation();
  const mfaMutation = useCompleteTOTPLoginMutation();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [challengeToken, setChallengeToken] = useState("");
  const [code, setCode] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const result = await mutation.mutateAsync({ username, password });
      setPassword("");
      if ("mfaRequired" in result) {
        setChallengeToken(result.challengeToken ?? "");
        return;
      }
      await navigate({ to: "/", replace: true });
    } catch {
      // The mutation error is rendered below without exposing server details.
    }
  }

  async function submitMFA(event: FormEvent) {
    event.preventDefault();
    try {
      await mfaMutation.mutateAsync({ challengeToken, code: code.toUpperCase() });
      setCode("");
      await navigate({ to: "/", replace: true });
    } catch {
      // The mutation error is rendered below without exposing server details.
    }
  }

  return (
    <main className="shell narrow">
      <div className="brand"><span className="mark">M</span><span>Molejo Console</span></div>
      <section className="card">
        <p className="eyebrow">Molejo</p>
        <h1>Entre para gerenciar seu Workspace</h1>
        {challengeToken ? <form onSubmit={submitMFA} className="stack">
          <p className="muted">Confirme o código do seu autenticador ou use um código de recuperação.</p>
          <Field label="Código de verificação" value={code} onChange={(event) => setCode(event.target.value.toUpperCase())} required autoComplete="one-time-code" autoFocus />
          {mfaMutation.isError && <Alert>{userFacingError(mfaMutation.error)}</Alert>}
          <Button type="submit" disabled={mfaMutation.isPending}>{mfaMutation.isPending ? "Verificando…" : "Verificar"}</Button>
          <Button type="button" variant="secondary" onClick={() => { setChallengeToken(""); setCode(""); }}>Voltar</Button>
        </form> : <form onSubmit={submit} className="stack">
          <Field label="Usuário" value={username} onChange={(event) => setUsername(event.target.value)} required autoComplete="username" autoFocus />
          <Field label="Senha" type="password" value={password} onChange={(event) => setPassword(event.target.value)} required autoComplete="current-password" />
          {mutation.isError && <Alert>{userFacingError(mutation.error)}</Alert>}
          <Button type="submit" disabled={mutation.isPending}>{mutation.isPending ? "Entrando…" : "Entrar"}</Button>
          <a href="/forgot-password">Esqueci minha senha</a>
        </form>}
      </section>
    </main>
  );
}
