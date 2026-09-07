import { FormEvent, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field } from "../../shared/ui/Field";
import { useCompleteTOTPLoginMutation, useLoginMutation } from "./model";
import { safeReturnTo } from "./return-to";

export function LoginPage() {
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as { returnTo?: string };
  const mutation = useLoginMutation();
  const mfaMutation = useCompleteTOTPLoginMutation();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [challengeToken, setChallengeToken] = useState("");
  const [code, setCode] = useState("");
  const [loginError, setLoginError] = useState<unknown>();
  const [mfaError, setMFAError] = useState<unknown>();
  const returnTo = safeReturnTo(search.returnTo);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setLoginError(undefined);
    try {
      const result = await mutation.mutateAsync({ username, password });
      if ("mfaRequired" in result) {
        setChallengeToken(result.challengeToken ?? "");
        return;
      }
      await navigate({ to: returnTo, replace: true });
    } catch (error) {
      setLoginError(error);
    } finally {
      setPassword("");
      mutation.reset();
    }
  }

  async function submitMFA(event: FormEvent) {
    event.preventDefault();
    setMFAError(undefined);
    try {
      await mfaMutation.mutateAsync({ challengeToken, code: code.toUpperCase() });
      setChallengeToken("");
      await navigate({ to: returnTo, replace: true });
    } catch (error) {
      setMFAError(error);
    } finally {
      setCode("");
      mfaMutation.reset();
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
          {mfaError !== undefined && <Alert>{userFacingError(mfaError)}</Alert>}
          <Button type="submit" disabled={mfaMutation.isPending}>{mfaMutation.isPending ? "Verificando…" : "Verificar"}</Button>
          <Button type="button" variant="secondary" onClick={() => { setChallengeToken(""); setCode(""); setMFAError(undefined); }}>Voltar</Button>
        </form> : <form onSubmit={submit} className="stack">
          <Field label="Usuário" value={username} onChange={(event) => setUsername(event.target.value)} required autoComplete="username" autoFocus />
          <Field label="Senha" type="password" value={password} onChange={(event) => setPassword(event.target.value)} required autoComplete="current-password" />
          {loginError !== undefined && <Alert>{userFacingError(loginError)}</Alert>}
          <Button type="submit" disabled={mutation.isPending}>{mutation.isPending ? "Entrando…" : "Entrar"}</Button>
          <a href="/forgot-password">Esqueci minha senha</a>
          <a href="/accept-invitation">Aceitar convite</a>
        </form>}
      </section>
    </main>
  );
}
