import { useState } from "react";
import {
  getOrCreateDriverInstallation,
  loginDriverWithPassword,
  loginDriverWithPasskey,
  registerPasskey,
  requestDriverActivation,
  driverLoginErrorMessage,
  type DriverActivationRequest,
  type DriverInstallation,
} from "./webauthn";
import {
  approveSupervisorDeviceRequest,
  listSupervisorDeviceAuthorizations,
  listSupervisorDeviceRequests,
  loginSupervisor,
  revokeSupervisorDevice,
  type DeviceAuthorizationView,
  type DeviceEnrollmentRequestView,
} from "./supervisor";

type ModuleCardProps = {
  name: string;
  description: string;
  status: "Preparado" | "No implementado";
  accent: string;
};

const modules: ModuleCardProps[] = [
  {
    name: "Drayage",
    description: "Bounded context para movimientos portuarios y formularios operativos.",
    status: "No implementado",
    accent: "border-emerald-200 bg-emerald-50",
  },
  {
    name: "Transportation",
    description: "Bounded context para planificación y viajes de larga distancia.",
    status: "No implementado",
    accent: "border-sky-200 bg-sky-50",
  },
  {
    name: "Dashboard",
    description: "Proyecciones autorizadas y consultas consolidadas por empresa.",
    status: "Preparado",
    accent: "border-violet-200 bg-violet-50",
  },
];

function ModuleCard({ name, description, status, accent }: ModuleCardProps) {
  return (
    <article className={["rounded-2xl", "border", "p-5", accent].join(" ")}>
      <div className="flex items-start justify-between gap-4">
        <h3 className="text-lg font-bold text-slate-950">{name}</h3>
        <span className="rounded-full bg-white/80 px-3 py-1 text-xs font-bold text-slate-700">
          {status}
        </span>
      </div>
      <p className="mt-3 text-sm leading-6 text-slate-600">{description}</p>
    </article>
  );
}
export default function App() {
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [companyID, setCompanyID] = useState("");
  const [operationID, setOperationID] = useState("");
  const [installation, setInstallation] = useState<DriverInstallation>();
  const [activationRequest, setActivationRequest] = useState<DriverActivationRequest>();
  const [registrationState, setRegistrationState] = useState<"idle" | "pending" | "approved" | "registering" | "registered" | "expired">("idle");
  const [status, setStatus] = useState("Listo para comenzar.");
  const [supervisorIdentifier, setSupervisorIdentifier] = useState("");
  const [supervisorPassword, setSupervisorPassword] = useState("");
  const [supervisorStatus, setSupervisorStatus] = useState("Supervisor no autenticado.");
  const [supervisorLoggedIn, setSupervisorLoggedIn] = useState(false);
  const [requests, setRequests] = useState<DeviceEnrollmentRequestView[]>([]);
  const [authorizations, setAuthorizations] = useState<DeviceAuthorizationView[]>([]);

  const refreshSupervisor = async () => {
    const [nextRequests, nextAuthorizations] = await Promise.all([
      listSupervisorDeviceRequests(),
      listSupervisorDeviceAuthorizations(),
    ]);
    setRequests(nextRequests);
    setAuthorizations(nextAuthorizations);
  };

  const supervisorLogin = async () => {
    try {
      await loginSupervisor(supervisorIdentifier, supervisorPassword);
      setSupervisorPassword("");
      await refreshSupervisor();
      setSupervisorLoggedIn(true);
      setSupervisorStatus("Sesión de supervisor activa. El alcance visible proviene de sus membresías y permisos.");
    } catch {
      setSupervisorStatus("No fue posible autenticar al supervisor o cargar sus dispositivos.");
    }
  };

  const approveRequest = async (requestID: string) => {
    try {
      await approveSupervisorDeviceRequest(requestID, "Aprobación de demo del cliente");
      await refreshSupervisor();
      setSupervisorStatus("Solicitud aprobada.");
    } catch {
      setSupervisorStatus("La solicitud no pudo aprobarse; su estado o alcance puede haber cambiado.");
    }
  };

  const revokeAuthorization = async (authorizationID: string) => {
    try {
      await revokeSupervisorDevice(authorizationID, "Revocación de demo del cliente");
      await refreshSupervisor();
      setSupervisorStatus("Autorización revocada. El próximo acceso del conductor debe ser rechazado.");
    } catch {
      setSupervisorStatus("La autorización no pudo revocarse; su estado o alcance puede haber cambiado.");
    }
  };

  const prepareInstallation = async () => {
    try {
      const existing = await getOrCreateDriverInstallation();
      setInstallation(existing);
      setStatus("Instalación preparada o recuperada desde IndexedDB. Los tokens de sesión no se almacenan allí.");
    } catch {
      setStatus("No fue posible preparar la instalación en este navegador.");
    }
  };

  const activate = async () => {
    if (!installation || !companyID) {
      setStatus("Prepara la instalación e indica la empresa.");
      return;
    }
    try {
      await loginDriverWithPassword(identifier, password);
      const request = await requestDriverActivation(companyID, operationID || undefined, installation);
      setActivationRequest(request);
      setRegistrationState("pending");
      setStatus(`Solicitud creada: ${request.correlationCode || "pendiente"}. Requiere aprobación del supervisor. Cuando sea aprobada, pulsa Registrar passkey antes de que expire.`);
    } catch {
      setStatus("La activación fue rechazada. La respuesta no revela si el conductor existe.");
    }
  };

  const registerApprovedPasskey = async () => {
    if (!installation || !activationRequest) {
      setStatus("Solicita primero la activación y espera la aprobación del supervisor.");
      return;
    }
    if (Date.parse(activationRequest.expiresAt) <= Date.now()) {
      setRegistrationState("expired");
      setStatus("La solicitud de activación expiró. Solicita una nueva; la instalación local se conserva.");
      return;
    }
    setRegistrationState("registering");
    setStatus("Verificando aprobación. Después Android solicitará la biometría o el PIN para registrar la passkey.");
    try {
      await registerPasskey("", activationRequest.id, (stage) => {
        if (stage === "options_received") {
          setRegistrationState("approved");
          setStatus("Solicitud aprobada. Confirma ahora la biometría o el PIN de Android.");
        }
      });
      setRegistrationState("registered");
      setStatus("Passkey registrada y dispositivo autorizado. Ya puedes ingresar con passkey.");
    } catch (error) {
      if (Date.parse(activationRequest.expiresAt) <= Date.now()) {
        setRegistrationState("expired");
        setStatus("La solicitud de activación expiró. Solicita una nueva; la instalación local se conserva.");
      } else if (error instanceof Error && error.name === "NotAllowedError") {
        setRegistrationState("pending");
        setStatus("Registro cancelado desde Android. La solicitud sigue disponible mientras no expire; puedes reintentarlo.");
      } else {
        setRegistrationState("pending");
        setStatus("La solicitud aún puede estar pendiente o haber sido rechazada. Verifica con el supervisor y reintenta mientras siga vigente.");
      }
    }
  };

  const recurringLogin = async () => {
    if (!installation) {
      setStatus("Prepara primero la instalación en este navegador.");
      return;
    }
    try {
      await loginDriverWithPasskey(identifier, installation);
      setStatus("Sesión DRIVER creada con WebAuthn y prueba de posesión de instalación.");
    } catch (error) {
      setStatus(driverLoginErrorMessage(error));
    }
  };

  return (
    <main className="min-h-screen bg-slate-950 px-5 py-8 text-slate-950 sm:px-8 sm:py-12">
      <div className="mx-auto max-w-6xl">
        <header className="rounded-3xl bg-white p-7 shadow-2xl shadow-slate-950/30 sm:p-10">
          <div className="flex flex-col gap-6 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <p className="text-xs font-black uppercase tracking-[0.22em] text-teal-700">
                Transportation Platform
              </p>
              <h1 className="mt-3 max-w-3xl text-4xl font-black tracking-tight text-slate-950 sm:text-5xl">
                Fundación técnica Fase 1A
              </h1>
              <p className="mt-5 max-w-2xl text-base leading-7 text-slate-600">Acceso móvil de conductores de Fase 1B.2-D.</p>
            </div>
            <div className="rounded-2xl border border-amber-200 bg-amber-50 px-5 py-4 text-sm font-semibold leading-6 text-amber-900">
              No hay formularios operativos, GPS ni modo offline en esta fase.
            </div>
          </div>
        </header>

        <section className="mt-6 grid gap-4 sm:grid-cols-3" aria-label="Estado de la plataforma">
          <div className="rounded-2xl border border-white/10 bg-white/10 p-5 text-white">
            <p className="text-xs font-bold uppercase tracking-[0.16em] text-teal-300">Backend</p>
            <p className="mt-2 text-xl font-black">Go net/http</p>
            <p className="mt-1 text-sm text-slate-300">Monolito modular</p>
          </div>
          <div className="rounded-2xl border border-white/10 bg-white/10 p-5 text-white">
            <p className="text-xs font-bold uppercase tracking-[0.16em] text-sky-300">Frontend</p>
            <p className="mt-2 text-xl font-black">React + Tailwind</p>
            <p className="mt-1 text-sm text-slate-300">TypeScript estricto</p>
          </div>
          <div className="rounded-2xl border border-white/10 bg-white/10 p-5 text-white">
            <p className="text-xs font-bold uppercase tracking-[0.16em] text-violet-300">Persistencia</p>
            <p className="mt-2 text-xl font-black">PostgreSQL externo</p>
            <p className="mt-1 text-sm text-slate-300">Sin conexión en esta fase</p>
          </div>
        </section>

        <section className="mt-6 rounded-3xl bg-white p-6 shadow-2xl shadow-slate-950/20 sm:p-8" aria-label="Acceso móvil de conductor">
          <p className="text-xs font-black uppercase tracking-[0.18em] text-slate-500">Acceso móvil</p>
          <h2 className="mt-2 text-2xl font-black text-slate-950">Activar o ingresar con passkey</h2>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-slate-600">
            La primera activación usa la identidad ya registrada y crea una solicitud limitada. Un supervisor debe aprobarla antes del registro de passkey.
          </p>
          <div className="mt-6 grid gap-4 md:grid-cols-2">
            <label className="text-sm font-semibold text-slate-700">Identificador<input className="mt-2 w-full rounded-xl border border-slate-300 p-3" value={identifier} onChange={(event) => setIdentifier(event.target.value)} autoComplete="username" /></label>
            <label className="text-sm font-semibold text-slate-700">Contraseña de activación<input className="mt-2 w-full rounded-xl border border-slate-300 p-3" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="current-password" /></label>
            <label className="text-sm font-semibold text-slate-700">Empresa<input className="mt-2 w-full rounded-xl border border-slate-300 p-3" value={companyID} onChange={(event) => setCompanyID(event.target.value)} placeholder="UUID autorizado" /></label>
            <label className="text-sm font-semibold text-slate-700">Operación opcional<input className="mt-2 w-full rounded-xl border border-slate-300 p-3" value={operationID} onChange={(event) => setOperationID(event.target.value)} placeholder="UUID autorizado" /></label>
          </div>
          <div className="mt-5 flex flex-wrap gap-3">
            <button className="rounded-xl bg-slate-900 px-4 py-3 text-sm font-bold text-white" onClick={() => void prepareInstallation()}>Preparar instalación</button>
            <button className="rounded-xl bg-teal-700 px-4 py-3 text-sm font-bold text-white" onClick={() => void activate()}>Solicitar activación</button>
            <button className="rounded-xl bg-amber-700 px-4 py-3 text-sm font-bold text-white disabled:cursor-not-allowed disabled:opacity-50" disabled={!activationRequest || registrationState === "registered"} onClick={() => void registerApprovedPasskey()}>{registrationState === "registered" ? "Passkey registrada" : "Registrar passkey"}</button>
            <button className="rounded-xl bg-violet-700 px-4 py-3 text-sm font-bold text-white" onClick={() => void recurringLogin()}>Ingresar con passkey</button>
          </div>
          {activationRequest && <p className="mt-4 rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900" aria-live="polite">
            {registrationState === "pending" && `Solicitud ${activationRequest.correlationCode || activationRequest.id} pendiente de aprobación. Expira a las ${new Date(activationRequest.expiresAt).toLocaleString()}.`}
            {registrationState === "approved" && "Solicitud aprobada; completa la confirmación de Android."}
            {registrationState === "registering" && "Registro de passkey en curso."}
            {registrationState === "registered" && "Registro completado; la autorización del dispositivo fue creada."}
            {registrationState === "expired" && "Solicitud expirada. Solicita una nueva activación; no se eliminó la instalación local."}
          </p>}
          <p className="mt-4 rounded-xl bg-slate-100 p-4 text-sm text-slate-700" role="status">{status}</p>
        </section>

        <section className="mt-6 rounded-3xl bg-white p-6 sm:p-8">
          <div>
            <p className="text-xs font-black uppercase tracking-[0.18em] text-slate-500">Navegación preparada</p>
            <h2 className="mt-2 text-2xl font-black text-slate-950">Módulos de negocio</h2>
            <p className="mt-2 max-w-2xl text-sm leading-6 text-slate-600">
              Los módulos sólo muestran la frontera futura. No exponen funcionalidades operativas implementadas.
            </p>
          </div>
          <div className="mt-6 grid gap-4 md:grid-cols-3">
            {modules.map((module) => (
              <ModuleCard key={module.name} {...module} />
            ))}
          </div>
        </section>

        <section className="mt-6 rounded-3xl bg-white p-6 shadow-2xl shadow-slate-950/20 sm:p-8" aria-label="Consola de supervisor">
          <p className="text-xs font-black uppercase tracking-[0.18em] text-slate-500">Demo supervisada</p>
          <h2 className="mt-2 text-2xl font-black text-slate-950">Aprobaciones de dispositivos</h2>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-600">
            Esta vista usa la sesión normal y los endpoints WebAuthn existentes. No habilita el API administrativo global ni acepta identidades desde headers.
          </p>
          <div className="mt-6 grid gap-4 md:grid-cols-[1fr_1fr_auto]">
            <label className="text-sm font-semibold text-slate-700">Usuario supervisor<input className="mt-2 w-full rounded-xl border border-slate-300 p-3" value={supervisorIdentifier} onChange={(event) => setSupervisorIdentifier(event.target.value)} autoComplete="username" /></label>
            <label className="text-sm font-semibold text-slate-700">Contraseña<input className="mt-2 w-full rounded-xl border border-slate-300 p-3" type="password" value={supervisorPassword} onChange={(event) => setSupervisorPassword(event.target.value)} autoComplete="current-password" /></label>
            <button className="self-end rounded-xl bg-slate-900 px-4 py-3 text-sm font-bold text-white" onClick={() => void supervisorLogin()}>Ingresar supervisor</button>
          </div>
          <p className="mt-4 rounded-xl bg-slate-100 p-4 text-sm text-slate-700" aria-live="polite">{supervisorStatus}</p>
          {supervisorLoggedIn && (
            <div className="mt-6 grid gap-6 lg:grid-cols-2">
              <div>
                <h3 className="text-lg font-black text-slate-950">Solicitudes pendientes ({requests.length})</h3>
                <div className="mt-3 space-y-3">
                  {requests.length === 0 && <p className="text-sm text-slate-500">No hay solicitudes visibles.</p>}
                  {requests.map((item) => (
                    <article className="rounded-2xl border border-amber-200 bg-amber-50 p-4" key={item.request_id}>
                      <p className="font-bold text-slate-950">{item.legal_name} · {item.identifier}</p>
                      <p className="mt-1 break-all text-xs text-slate-600">Empresa {item.company_id} · Operación {item.company_operation_id ?? "general"}</p>
                      <p className="mt-1 text-xs text-slate-600">Código {item.correlation_code} · Instalación {item.installation_id}</p>
                      <button className="mt-3 rounded-lg bg-amber-700 px-3 py-2 text-xs font-bold text-white disabled:cursor-not-allowed disabled:opacity-50" disabled={!item.request_id} onClick={() => void approveRequest(item.request_id)}>Aprobar</button>
                    </article>
                  ))}
                </div>
              </div>
              <div>
                <h3 className="text-lg font-black text-slate-950">Dispositivos autorizados ({authorizations.length})</h3>
                <div className="mt-3 space-y-3">
                  {authorizations.length === 0 && <p className="text-sm text-slate-500">No hay dispositivos visibles.</p>}
                  {authorizations.map((item) => (
                    <article className="rounded-2xl border border-emerald-200 bg-emerald-50 p-4" key={item.authorization_id}>
                      <p className="font-bold text-slate-950">{item.legal_name} · {item.identifier}</p>
                      <p className="mt-1 break-all text-xs text-slate-600">{item.status} · Empresa {item.company_id} · Operación {item.company_operation_id ?? "general"}</p>
                      <p className="mt-1 text-xs text-slate-600">Instalación {item.installation_id} · {item.key_algorithm}</p>
                      {item.status === "ACTIVE" && <button className="mt-3 rounded-lg bg-rose-700 px-3 py-2 text-xs font-bold text-white disabled:cursor-not-allowed disabled:opacity-50" disabled={!item.authorization_id} onClick={() => void revokeAuthorization(item.authorization_id)}>Revocar</button>}
                    </article>
                  ))}
                </div>
              </div>
            </div>
          )}
        </section>
      </div>
    </main>
  );
}
