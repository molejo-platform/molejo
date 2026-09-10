import type { SetupStep } from "./model";
import styles from "./ApplicationSetupProgress.module.css";

const steps = ["Aplicação", "Execução", "Revisão"];

export function ApplicationSetupProgress({ step }: { step: SetupStep }) {
  return (
    <>
      <div>
        <p className="eyebrow">Novo App no Environment</p>
        <h3>Configure somente o necessário para começar</h3>
        <p className="muted">O rascunho fica salvo nesta sessão até a configuração ser concluída.</p>
      </div>
      <ol className={styles.steps} aria-label="Etapas da configuração">
        {steps.map((label, index) => (
          <li key={label} aria-current={step === index + 1 ? "step" : undefined} data-complete={step > index + 1}>
            <span>{index + 1}</span> {label}
          </li>
        ))}
      </ol>
    </>
  );
}
