import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useEffect, useState, type FormEvent } from "react";

import { userFacingError } from "../../shared/api/errors";
import { clearSessionState } from "../../shared/auth/session-state";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { Field } from "../../shared/ui/Field";
import { PageHeader } from "../../shared/ui/Page";
import { useSessionQuery } from "../auth/model";
import { beginTOTPEnrollment, changePassword, completePasswordReset, confirmTOTPEnrollment, disableTOTP, getMFAStatus, identityKeys, listSessions, requestPasswordReset, revokeSession, updateProfile, verifyPasswordReset } from "./api";

export function AccountPage() {
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const user = session.data?.user;
  const [displayName, setDisplayName] = useState(user?.displayName ?? "");
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  useEffect(() => { if (user) setDisplayName(user.displayName); }, [user?.displayName]);
  const profile = useMutation({ mutationFn: () => updateProfile(user!, displayName), onSuccess: () => queryClient.invalidateQueries({ queryKey: ["session"] }) });
  const password = useMutation({ mutationFn: () => changePassword(currentPassword, newPassword), onSuccess: async () => { clearSessionState(queryClient); await navigate({ to: "/login", replace: true }); } });
  const sessions = useQuery({ queryKey: identityKeys.sessions, queryFn: listSessions });
  const revoke = useMutation({ mutationFn: revokeSession, onSuccess: () => queryClient.invalidateQueries({ queryKey: identityKeys.sessions }) });
  if (!user) return null;
  return <div className="stack constrained"><PageHeader eyebrow="Conta" title="Perfil e segurança" description="Sua identidade existe independentemente dos Workspaces." breadcrumbs={[{ label: "Visão geral", to: "/" }, { label: "Conta" }]}/>
    <section className="panel stack"><div><h2>Perfil</h2><p className="muted">O username é estável; o nome de exibição pode ser alterado.</p></div><form className="form-row" onSubmit={(event) => { event.preventDefault(); profile.mutate(); }}><Field label="Username" value={user.username} disabled/><Field label="Nome de exibição" value={displayName} onChange={(event) => setDisplayName(event.target.value)} maxLength={120} required/><Button type="submit" loading={profile.isPending} disabled={displayName.trim() === user.displayName}>Salvar perfil</Button></form>{profile.isSuccess && <Alert tone="success">Perfil atualizado.</Alert>}{profile.isError && <Alert>{userFacingError(profile.error)}</Alert>}</section>
    <section className="panel stack"><div><h2>Trocar senha</h2><p className="muted">A alteração encerra todas as sessões, inclusive esta.</p></div><form className="form-row" onSubmit={(event) => { event.preventDefault(); password.mutate(); }}><Field label="Senha atual" type="password" autoComplete="current-password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} required/><Field label="Nova senha" helper="Use ao menos 15 caracteres." type="password" autoComplete="new-password" minLength={15} value={newPassword} onChange={(event) => setNewPassword(event.target.value)} required/><Button type="submit" loading={password.isPending}>Trocar senha</Button></form>{password.isError && <Alert>{userFacingError(password.error)}</Alert>}</section>
    <TOTPSection />
    <section className="panel stack"><div><h2>Sessões ativas</h2><p className="muted">Revogue acessos que você não reconhece.</p></div>{sessions.isPending ? <p role="status">Carregando sessões…</p> : sessions.data?.items.map((item) => <div className="data-row" key={item.id}><span><strong>{item.current ? "Esta sessão" : item.id}</strong><small>{item.assuranceLevel} · vista em {formatDateTime(item.lastSeenAt)} · expira em {formatDateTime(item.expiresAt)}</small></span><ConfirmAction trigger="Revogar" title="Revogar esta sessão?" description={item.current ? "Você será desconectado imediatamente." : "O dispositivo precisará autenticar novamente."} confirmLabel="Revogar sessão" pending={revoke.isPending} onConfirm={async () => { await revoke.mutateAsync(item.id); if (item.current) { clearSessionState(queryClient); await navigate({ to: "/login", replace: true }); } }}/></div>)}{sessions.isError && <Alert>{userFacingError(sessions.error)}</Alert>}</section>
  </div>;
}

function TOTPSection() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const status = useQuery({ queryKey: identityKeys.mfa, queryFn: getMFAStatus });
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [enrollment, setEnrollment] = useState<{ challengeToken: string; secret: string; otpAuthUrl: string } | null>(null);
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
  const begin = useMutation({ mutationFn: () => beginTOTPEnrollment(password), onSuccess: (value) => { setEnrollment(value as typeof enrollment); setPassword(""); } });
  const confirm = useMutation({ mutationFn: () => confirmTOTPEnrollment(enrollment!.challengeToken!, code), onSuccess: (value) => { setRecoveryCodes(value.recoveryCodes); setEnrollment(null); setCode(""); queryClient.invalidateQueries({ queryKey: identityKeys.mfa }); } });
  const disable = useMutation({ mutationFn: () => disableTOTP(password), onSuccess: async () => { setPassword(""); clearSessionState(queryClient); await navigate({ to: "/login", replace: true }); } });
  const error = status.error ?? begin.error ?? confirm.error ?? disable.error;
  return <section className="panel stack"><div><h2>Verificação em duas etapas</h2><p className="muted">TOTP adiciona uma segunda prova ao login. Os segredos ficam no backend seguro da instalação.</p></div>
    {status.isPending ? <p role="status">Carregando segurança…</p> : status.data?.totpEnabled ? <form className="form-row" onSubmit={(event) => { event.preventDefault(); disable.mutate(); }}><Field label="Senha atual" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required/><Button type="submit" variant="danger" loading={disable.isPending}>Desativar TOTP</Button></form> : enrollment ? <form className="stack" onSubmit={(event) => { event.preventDefault(); confirm.mutate(); }}><Alert tone="info">Adicione a chave no autenticador e confirme um código. A chave é exibida somente durante este cadastro.</Alert><Field label="Chave TOTP" value={enrollment.secret ?? ""} readOnly/><a href={enrollment.otpAuthUrl}>Abrir no autenticador</a><Field label="Código de seis dígitos" value={code} onChange={(event) => setCode(event.target.value)} inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" required/><Button type="submit" loading={confirm.isPending}>Confirmar e ativar</Button></form> : <form className="form-row" onSubmit={(event) => { event.preventDefault(); begin.mutate(); }}><Field label="Confirme sua senha" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required/><Button type="submit" loading={begin.isPending}>Configurar TOTP</Button></form>}
    {recoveryCodes.length > 0 && <Alert tone="warning"><strong>Guarde estes códigos agora.</strong><br/>{recoveryCodes.map((item) => <span className="mono" key={item}>{item}<br/></span>)}</Alert>}
    {error && <Alert>{userFacingError(error)}</Alert>}
  </section>;
}

export function ForgotPasswordPage() {
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [code, setCode] = useState("");
  const [ticket, setTicket] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const requestReset = useMutation({ mutationFn: () => requestPasswordReset(username) });
  const verify = useMutation({ mutationFn: () => verifyPasswordReset(username, code), onSuccess: (result) => setTicket(result.ticket) });
  const complete = useMutation({ mutationFn: () => completePasswordReset(username, ticket, newPassword), onSuccess: () => navigate({ to: "/login", replace: true }) });
  const submitRequest = (event: FormEvent) => { event.preventDefault(); requestReset.mutate(); };
  return <main className="shell narrow"><div className="brand"><span className="mark">M</span><span>Molejo Console</span></div><section className="card stack"><p className="eyebrow">Recuperação de acesso</p><h1>Redefinir senha</h1>
    {!requestReset.isSuccess ? <form className="stack" onSubmit={submitRequest}><Field label="Username" value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" required/><p className="muted">Um administrador deverá gerar e entregar o código temporário. A resposta não confirma se o usuário existe.</p><Button type="submit" loading={requestReset.isPending}>Continuar</Button></form> : !ticket ? <form className="stack" onSubmit={(event) => { event.preventDefault(); verify.mutate(); }}><Alert tone="info">Solicitação registrada. Digite o código temporário fornecido pela administração.</Alert><Field label="Código temporário" value={code} onChange={(event) => setCode(event.target.value.toUpperCase())} autoComplete="one-time-code" required/><Button type="submit" loading={verify.isPending}>Verificar código</Button>{verify.isError && <Alert>{userFacingError(verify.error)}</Alert>}</form> : <form className="stack" onSubmit={(event) => { event.preventDefault(); complete.mutate(); }}><Field label="Nova senha" helper="Use ao menos 15 caracteres." type="password" minLength={15} value={newPassword} onChange={(event) => setNewPassword(event.target.value)} autoComplete="new-password" required/><Button type="submit" loading={complete.isPending}>Salvar nova senha</Button>{complete.isError && <Alert>{userFacingError(complete.error)}</Alert>}</form>}
    {requestReset.isError && <Alert>{userFacingError(requestReset.error)}</Alert>}<Link to="/login">Voltar ao login</Link></section></main>;
}
