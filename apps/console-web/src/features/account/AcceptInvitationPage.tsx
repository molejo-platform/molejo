import { useMutation } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { BrandLogo } from "../../shared/ui/BrandLogo";
import { Button } from "../../shared/ui/Button";
import { Field } from "../../shared/ui/Field";
import { PageFrame } from "../../shared/ui/PageFrame";
import { acceptUserInvitation } from "./api";

export function AcceptInvitationPage() {
  const navigate = useNavigate();
  const [token, setToken] = useState("");
  const [password, setPassword] = useState("");
  const [actionError, setActionError] = useState<unknown>();
  const accept = useMutation({ mutationFn: () => acceptUserInvitation(token, password), gcTime: 0 });
  async function submit(event: FormEvent) {
    event.preventDefault();
    setActionError(undefined);
    try {
      await accept.mutateAsync();
      setToken("");
      setPassword("");
      await navigate({ to: "/login", search: { returnTo: "/" }, replace: true });
    } catch (error) {
      setActionError(error);
    } finally {
      accept.reset();
    }
  }
  return (
    <PageFrame as="main" width="form" className="shell">
      <div className="brand">
        <BrandLogo surface="light" />
        <span className="brand-product-name">Console</span>
      </div>
      <section className="card stack">
        <p className="eyebrow">Ativação de acesso</p>
        <h1>Aceitar convite</h1>
        <p className="muted">Use o token entregue pela administração e defina sua senha.</p>
        <form className="stack" onSubmit={submit}>
          <Field
            label="Token do convite"
            value={token}
            onChange={(event) => setToken(event.target.value.trim())}
            autoComplete="one-time-code"
            required
          />
          <Field
            label="Nova senha"
            helper="Use ao menos 15 caracteres."
            type="password"
            minLength={15}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            autoComplete="new-password"
            required
          />
          <Button type="submit" loading={accept.isPending}>
            Ativar conta
          </Button>
        </form>
        {actionError !== undefined && <Alert>{userFacingError(actionError)}</Alert>}
        <Link to="/login" search={{ returnTo: "/" }}>
          Voltar ao login
        </Link>
      </section>
    </PageFrame>
  );
}
