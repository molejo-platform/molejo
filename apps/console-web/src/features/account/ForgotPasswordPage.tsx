import { useMutation } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { BrandLogo } from "../../shared/ui/BrandLogo";
import { Button } from "../../shared/ui/Button";
import { Field } from "../../shared/ui/Field";
import { PageFrame } from "../../shared/ui/PageFrame";
import { completePasswordReset, requestPasswordReset, verifyPasswordReset } from "./api";

export function ForgotPasswordPage() {
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [code, setCode] = useState("");
  const [ticket, setTicket] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [verifyError, setVerifyError] = useState<unknown>();
  const [completeError, setCompleteError] = useState<unknown>();
  const requestReset = useMutation({ mutationFn: () => requestPasswordReset(username) });
  const verify = useMutation({ mutationFn: () => verifyPasswordReset(username, code), gcTime: 0 });
  const complete = useMutation({ mutationFn: () => completePasswordReset(username, ticket, newPassword), gcTime: 0 });
  const submitRequest = (event: FormEvent) => {
    event.preventDefault();
    requestReset.mutate();
  };
  const submitVerification = async (event: FormEvent) => {
    event.preventDefault();
    setVerifyError(undefined);
    try {
      setTicket((await verify.mutateAsync()).ticket);
      setCode("");
    } catch (error) {
      setVerifyError(error);
    } finally {
      verify.reset();
    }
  };
  const submitNewPassword = async (event: FormEvent) => {
    event.preventDefault();
    setCompleteError(undefined);
    try {
      await complete.mutateAsync();
      setNewPassword("");
      await navigate({ to: "/login", search: { returnTo: "/" }, replace: true });
    } catch (error) {
      setCompleteError(error);
    } finally {
      complete.reset();
    }
  };
  return (
    <PageFrame as="main" width="form" className="shell">
      <div className="brand">
        <BrandLogo surface="light" />
        <span className="brand-product-name">Console</span>
      </div>
      <section className="card stack">
        <p className="eyebrow">Recuperação de acesso</p>
        <h1>Redefinir senha</h1>
        {!requestReset.isSuccess ? (
          <form className="stack" onSubmit={submitRequest}>
            <Field
              label="Username"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              autoComplete="username"
              required
            />
            <p className="muted">
              Um administrador deverá gerar e entregar o código temporário. A resposta não confirma se o usuário existe.
            </p>
            <Button type="submit" loading={requestReset.isPending}>
              Continuar
            </Button>
          </form>
        ) : !ticket ? (
          <form className="stack" onSubmit={submitVerification}>
            <Alert tone="info">Solicitação registrada. Digite o código temporário fornecido pela administração.</Alert>
            <Field
              label="Código temporário"
              value={code}
              onChange={(event) => setCode(event.target.value.toUpperCase())}
              autoComplete="one-time-code"
              required
            />
            <Button type="submit" loading={verify.isPending}>
              Verificar código
            </Button>
            {verifyError !== undefined && <Alert>{userFacingError(verifyError)}</Alert>}
          </form>
        ) : (
          <form className="stack" onSubmit={submitNewPassword}>
            <Field
              label="Nova senha"
              helper="Use ao menos 15 caracteres."
              type="password"
              minLength={15}
              value={newPassword}
              onChange={(event) => setNewPassword(event.target.value)}
              autoComplete="new-password"
              required
            />
            <Button type="submit" loading={complete.isPending}>
              Salvar nova senha
            </Button>
            {completeError !== undefined && <Alert>{userFacingError(completeError)}</Alert>}
          </form>
        )}
        {requestReset.isError && <Alert>{userFacingError(requestReset.error)}</Alert>}
        <Link to="/login" search={{ returnTo: "/" }}>
          Voltar ao login
        </Link>
      </section>
    </PageFrame>
  );
}
