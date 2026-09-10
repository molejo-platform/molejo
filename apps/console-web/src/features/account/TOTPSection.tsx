import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import { clearSessionState } from "../../shared/auth/session-state";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field } from "../../shared/ui/Field";
import { useAuthenticationCapabilitiesQuery } from "../authentication/public";
import { accountKeys, beginTOTPEnrollment, confirmTOTPEnrollment, disableTOTP, getMFAStatus } from "./api";

export function TOTPSection() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const capabilities = useAuthenticationCapabilitiesQuery();
  const status = useQuery({ queryKey: accountKeys.mfa, queryFn: getMFAStatus });
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [enrollment, setEnrollment] = useState<{ challengeToken: string; secret: string; otpAuthUrl: string } | null>(
    null,
  );
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
  const [actionError, setActionError] = useState<unknown>();
  const begin = useMutation({ mutationFn: () => beginTOTPEnrollment(password), gcTime: 0 });
  const confirm = useMutation({
    mutationFn: () => confirmTOTPEnrollment(enrollment!.challengeToken!, code),
    gcTime: 0,
  });
  const disable = useMutation({ mutationFn: () => disableTOTP(password), gcTime: 0 });
  async function beginEnrollment(event: FormEvent) {
    event.preventDefault();
    setActionError(undefined);
    try {
      setEnrollment(await begin.mutateAsync());
    } catch (error) {
      setActionError(error);
    } finally {
      setPassword("");
      begin.reset();
    }
  }
  async function confirmEnrollment(event: FormEvent) {
    event.preventDefault();
    setActionError(undefined);
    try {
      const result = await confirm.mutateAsync();
      setRecoveryCodes(result.recoveryCodes);
      setEnrollment(null);
      await queryClient.invalidateQueries({ queryKey: accountKeys.mfa });
    } catch (error) {
      setActionError(error);
    } finally {
      setCode("");
      confirm.reset();
    }
  }
  async function disableEnrollment(event: FormEvent) {
    event.preventDefault();
    setActionError(undefined);
    try {
      await disable.mutateAsync();
      clearSessionState(queryClient);
      await navigate({ to: "/login", search: { returnTo: "/" }, replace: true });
    } catch (error) {
      setActionError(error);
    } finally {
      setPassword("");
      disable.reset();
    }
  }
  const error = capabilities.error ?? status.error ?? actionError;
  return (
    <section className="panel stack">
      <div>
        <h2>Verificação em duas etapas</h2>
        <p className="muted">
          TOTP adiciona uma segunda prova ao login. Os segredos ficam no backend seguro da instalação.
        </p>
      </div>
      {status.isPending || capabilities.isPending ? (
        <p role="status">Carregando segurança…</p>
      ) : status.data?.totpEnabled ? (
        <form className="inline-form" onSubmit={disableEnrollment}>
          <Field
            label="Senha atual"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            required
          />
          <Button type="submit" variant="danger" loading={disable.isPending}>
            Desativar TOTP
          </Button>
        </form>
      ) : !capabilities.data?.totp ? (
        <Alert tone="info">TOTP não está habilitado nesta instalação.</Alert>
      ) : enrollment ? (
        <form className="stack" onSubmit={confirmEnrollment}>
          <Alert tone="info">
            Adicione a chave no autenticador e confirme um código. A chave é exibida somente durante este cadastro.
          </Alert>
          <Field label="Chave TOTP" value={enrollment.secret ?? ""} readOnly />
          <a href={enrollment.otpAuthUrl}>Abrir no autenticador</a>
          <Field
            label="Código de seis dígitos"
            value={code}
            onChange={(event) => setCode(event.target.value)}
            inputMode="numeric"
            autoComplete="one-time-code"
            pattern="[0-9]{6}"
            required
          />
          <Button type="submit" loading={confirm.isPending}>
            Confirmar e ativar
          </Button>
        </form>
      ) : (
        <form className="inline-form" onSubmit={beginEnrollment}>
          <Field
            label="Confirme sua senha"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            required
          />
          <Button type="submit" loading={begin.isPending}>
            Configurar TOTP
          </Button>
        </form>
      )}
      {recoveryCodes.length > 0 && (
        <Alert tone="warning">
          <strong>Guarde estes códigos agora.</strong>
          <br />
          {recoveryCodes.map((item) => (
            <span className="mono" key={item}>
              {item}
              <br />
            </span>
          ))}
        </Alert>
      )}
      {error !== null && error !== undefined && <Alert>{userFacingError(error)}</Alert>}
    </section>
  );
}
