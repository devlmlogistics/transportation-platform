import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { DriverManagement } from "./DriverManagement";
import {
  driverLoginErrorMessage,
  associateDriverInstallationIdentifier,
  clearDriverActivationRequest,
  clearDriverInstallationIdentifier,
  getOrCreateDriverInstallation,
  loadDriverActivationRequest,
  loadDriverInstallation,
  loginDriverWithPasskey,
  loginDriverWithPassword,
  persistDriverActivationRequest,
  queryDriverActivationStatus,
  registerPasskey,
  requestDriverActivation,
  DriverActivationRequestConflictError,
  DriverActivationStatusError,
  type DriverActivationRequest,
  type DriverActivationRequestStatus,
  type DriverInstallation,
} from "./webauthn";
import {
  approveSupervisorDeviceRequest,
  listSupervisorDeviceAuthorizations,
  listSupervisorDeviceRequests,
  loginSupervisor,
  revokeSupervisorDevice,
  SupervisorRequestError,
  type DeviceAuthorizationView,
  type DeviceEnrollmentRequestView,
} from "./supervisor";
import {
  consumeDriverInvitation,
  readInvitationTokenFromLocation,
  validateDriverInvitation,
  type DriverInvitationPreview,
} from "./driverInvitation";
import {
  createDriverEnrollmentInvitation,
  listDriverEnrollmentCandidates,
  type DriverEnrollmentCandidate,
} from "./supervisor";
import { demoCompanies, findDemoCompany, findDemoOperation } from "./demoCatalog";

type ModuleCardProps = { name: string; description: string; status: "Disponible" | "Próximamente"; accent: string };
const modules: ModuleCardProps[] = [
  { name: "Drayage", description: "Movimientos portuarios y coordinación de equipos en un espacio preparado para crecer.", status: "Próximamente", accent: "border-emerald-200 bg-emerald-50" },
  { name: "Transportation", description: "Viajes de larga distancia y planificación operacional como siguiente contexto de negocio.", status: "Próximamente", accent: "border-sky-200 bg-sky-50" },
  { name: "Seguridad de acceso", description: "Passkeys, aprobación supervisada y revocación inmediata para proteger cada instalación.", status: "Disponible", accent: "border-violet-200 bg-violet-50" },
];

type RegistrationState = "idle" | "pending" | "approved" | "registering" | "registered" | "expired" | "rejected" | "revoked";
type InstallationState = "checking" | "missing" | "ready" | "error";

const formatDate = (value: string | null): string => {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? "—" : date.toLocaleString();
};

const statusLabel = (status: string): string => {
  switch (status.toUpperCase()) {
    case "ACTIVE": return "Activo";
    case "REVOKED": return "Revocado";
    case "PENDING": return "Pendiente";
    case "APPROVED": return "Aprobado";
    case "EXPIRED": return "Expirada";
    case "REJECTED": return "Rechazado";
    default: return status || "Sin estado";
  }
};

const companyName = (id: string): string => findDemoCompany(id)?.name ?? "Empresa autorizada";
const operationName = (id: string | null): string => id ? (findDemoOperation(id)?.name ?? "Operación autorizada") : "Alcance empresarial";
const effectiveRequestStatus = (item: DeviceEnrollmentRequestView): string => {
  const status = item.status.toUpperCase();
  const expiresAt = Date.parse(item.expires_at);
  return ["PENDING", "APPROVED"].includes(status) && (!Number.isFinite(expiresAt) || expiresAt <= Date.now()) ? "EXPIRED" : status;
};

const registrationStateFor = (requestStatus: string, authorizationStatus: string | null = null, expiresAt?: string): RegistrationState => {
  if (authorizationStatus?.toUpperCase() === "REVOKED") return "revoked";
  if (["PENDING", "APPROVED"].includes(requestStatus.toUpperCase()) && expiresAt !== undefined) {
    const expiry = Date.parse(expiresAt);
    if (!Number.isFinite(expiry) || expiry <= Date.now()) return "expired";
  }
  switch (requestStatus.toUpperCase()) {
    case "APPROVED": return "approved";
    case "CONSUMED": return "registered";
    case "EXPIRED": return "expired";
    case "REJECTED": return "rejected";
    case "REVOKED": return "revoked";
    case "PENDING": return "pending";
    default: return "idle";
  }
};

function ModuleCard({ name, description, status, accent }: ModuleCardProps) {
  return <article className={["rounded-2xl", "border", "p-5", accent].join(" ")}>
    <div className="flex items-start justify-between gap-4"><h3 className="text-lg font-bold text-slate-950">{name}</h3><span className="rounded-full bg-white/80 px-3 py-1 text-xs font-bold text-slate-700">{status}</span></div>
    <p className="mt-3 text-sm leading-6 text-slate-600">{description}</p>
  </article>;
}

function StepList({ registrationState, installationState }: { registrationState: RegistrationState; installationState: InstallationState }) {
  const steps = [
    { label: "Identidad", complete: registrationState !== "idle" },
    { label: "Instalación", complete: installationState === "ready" },
    { label: "Aprobación", complete: ["approved", "registering", "registered"].includes(registrationState) },
    { label: "Passkey", complete: registrationState === "registered" },
  ];
  return <ol className="grid grid-cols-2 gap-2 sm:grid-cols-4" aria-label="Pasos de activación">
    {steps.map((step, index) => <li key={step.label} className={["rounded-xl", "border", "px-3", "py-3", "text-sm", step.complete ? "border-teal-200 bg-teal-50 text-teal-900" : "border-slate-200 bg-slate-50 text-slate-500"].join(" ")}>
      <span className="mr-2 font-black">{step.complete ? "✓" : index + 1}</span>{step.label}
    </li>)}
  </ol>;
}

export default function App() {
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [companyID, setCompanyID] = useState(demoCompanies[0]?.id ?? "");
  const [operationID, setOperationID] = useState("");
  const [installation, setInstallation] = useState<DriverInstallation>();
  const [installationState, setInstallationState] = useState<InstallationState>("checking");
  const [activationRequest, setActivationRequest] = useState<DriverActivationRequest>();
  const [registrationState, setRegistrationState] = useState<RegistrationState>("idle");
  const [driverStatusRefreshing, setDriverStatusRefreshing] = useState(false);
  const [status, setStatus] = useState("Listo para comenzar.");
  const [driverBusy, setDriverBusy] = useState<"prepare" | "activate" | "register" | "login" | null>(null);
  const [supervisorIdentifier, setSupervisorIdentifier] = useState("");
  const [supervisorPassword, setSupervisorPassword] = useState("");
  const [supervisorStatus, setSupervisorStatus] = useState("Supervisor no autenticado.");
  const [supervisorLoggedIn, setSupervisorLoggedIn] = useState(false);
  const [supervisorRefreshing, setSupervisorRefreshing] = useState(false);
  const [supervisorPollingBlocked, setSupervisorPollingBlocked] = useState(false);
  const [supervisorNotice, setSupervisorNotice] = useState<string | null>(null);
  const [requests, setRequests] = useState<DeviceEnrollmentRequestView[]>([]);
  const [authorizations, setAuthorizations] = useState<DeviceAuthorizationView[]>([]);
  const [supervisorAction, setSupervisorAction] = useState<string | null>(null);
  const [driverInviteToken, setDriverInviteToken] = useState<string | null>(null);
  const [driverInvitePreview, setDriverInvitePreview] = useState<DriverInvitationPreview | null>(null);
  const [driverInviteCode, setDriverInviteCode] = useState("");
  const [driverInviteStatus, setDriverInviteStatus] = useState<string | null>(null);
  const [driverInviteBusy, setDriverInviteBusy] = useState(false);
  const [supervisorCandidates, setSupervisorCandidates] = useState<DriverEnrollmentCandidate[]>([]);
  const [candidateError, setCandidateError] = useState<string | null>(null);
  const [candidateRevision, setCandidateRevision] = useState(0);
  const [selectedInviteDriverID, setSelectedInviteDriverID] = useState("");
  const [issuedInviteLink, setIssuedInviteLink] = useState<string | null>(null);
  const [issuedInviteCode, setIssuedInviteCode] = useState<string | null>(null);
  const supervisorActionRef = useRef<string | null>(null);
  const supervisorPollingBlockedRef = useRef(false);
  const supervisorRefreshRef = useRef<Promise<void> | null>(null);
  const supervisorAbortRef = useRef<AbortController | null>(null);
  const supervisorRefreshFailedRef = useRef(false);
  const supervisorRefreshSequenceRef = useRef(0);
  const supervisorSnapshotReadyRef = useRef(false);
  const knownRequestIDsRef = useRef(new Set<string>());
  const supervisorNoticeTimerRef = useRef<number | undefined>(undefined);
  const driverStatusInFlightRef = useRef<Promise<void> | null>(null);
  const driverStatusSequenceRef = useRef(0);
  const driverStatusPollRef = useRef<((force?: boolean) => Promise<void>) | null>(null);
  const mountedRef = useRef(true);

  const updateSupervisorAction = (action: string | null) => {
    supervisorActionRef.current = action;
    setSupervisorAction(action);
  };

  const selectedCompany = useMemo(() => findDemoCompany(companyID), [companyID]);
  const activeAuthorizations = authorizations.filter((item) => item.status.toUpperCase() === "ACTIVE");
  const revokedAuthorizations = authorizations.filter((item) => item.status.toUpperCase() === "REVOKED");
  const recognizedDriver = Boolean(installation?.identifier);
  const isExpired = (item: DeviceEnrollmentRequestView) => !Number.isFinite(Date.parse(item.expires_at)) || Date.parse(item.expires_at) <= Date.now();
  const pendingRequests = requests.filter((item) => item.status.toUpperCase() === "PENDING" && !isExpired(item));
  const approvedRequests = requests.filter((item) => item.status.toUpperCase() === "APPROVED" && !isExpired(item));
  const closedRequests = requests.filter((item) => ["EXPIRED", "REJECTED"].includes(item.status.toUpperCase()) || (["PENDING", "APPROVED"].includes(item.status.toUpperCase()) && isExpired(item)));

  useEffect(() => {
    let mounted = true;
    void loadDriverInstallation().then((stored) => {
      if (!mounted) return;
      if (stored) {
        setInstallation(stored);
        if (stored.identifier) setIdentifier(stored.identifier);
        setInstallationState("ready");
        void loadDriverActivationRequest(stored.id).then((savedRequest) => {
          if (!mounted || !savedRequest) return;
          setActivationRequest(savedRequest);
           if (!stored.identifier && savedRequest.identifier) setIdentifier(savedRequest.identifier);
          setRegistrationState(registrationStateFor(savedRequest.status, null, savedRequest.expiresAt));
          setStatus("Instalación y solicitud recuperadas de forma segura.");
        }).catch(() => {
          if (mounted) setStatus("Instalación recuperada. No se pudo cargar la solicitud local.");
        });
        setStatus("Instalación recuperada de forma segura. Puedes ingresar o consultar tu activación.");
      } else {
        setInstallationState("missing");
      }
    }).catch(() => {
      if (mounted) setInstallationState("error");
    });
    return () => { mounted = false; };
  }, []);

  useEffect(() => {
    const token = readInvitationTokenFromLocation();
    if (!token) return;
    setDriverInviteToken(token);
    setDriverInviteStatus("Validando invitación segura…");
    void validateDriverInvitation(token).then((preview) => {
      if (!mountedRef.current) return;
      setDriverInvitePreview(preview);
      setCompanyID(preview.company_id);
      if (preview.company_operation_id) setOperationID(preview.company_operation_id);
      setDriverInviteStatus("Invitación válida. Confirma el código recibido por el supervisor.");
    }).catch(() => {
      if (mountedRef.current) setDriverInviteStatus("La invitación no es válida, ya fue usada o venció.");
    });
  }, []);

  useEffect(() => {
    if (!supervisorLoggedIn || !companyID) return;
    const controller = new AbortController();
    setSupervisorCandidates([]);
    setSelectedInviteDriverID("");
    setCandidateError(null);
    void listDriverEnrollmentCandidates(companyID, operationID || undefined, controller.signal).then((candidates) => {
      if (!mountedRef.current || controller.signal.aborted) return;
      setSupervisorCandidates(candidates);
      setSelectedInviteDriverID((current) => current && candidates.some((item) => item.person_id === current) ? current : candidates[0]?.person_id ?? "");
    }).catch((error) => {
      if (!mountedRef.current || controller.signal.aborted) return;
      setSupervisorCandidates([]);
      setCandidateError(error instanceof SupervisorRequestError && error.status === 403
        ? "No tienes permisos para consultar conductores en este alcance."
        : error instanceof SupervisorRequestError && error.status === 401
          ? "Tu sesión ha expirado. Vuelve a ingresar."
          : "No fue posible cargar los conductores. Vuelve a ingresar o selecciona el alcance nuevamente.");
    });
    return () => controller.abort();
  }, [companyID, operationID, supervisorLoggedIn, candidateRevision]);

  useEffect(() => {
    const installationID = installation?.id;
    const requestID = activationRequest?.id;
    if (!installationID || !requestID || registrationState !== "pending") {
      driverStatusPollRef.current = null;
      return;
    }

    let cancelled = false;
    let stopAutomaticPolling = false;
    const poll = async (force = false): Promise<void> => {
      if (cancelled || !mountedRef.current || document.visibilityState !== "visible" || (!force && stopAutomaticPolling)) return;
      if (driverStatusInFlightRef.current) return driverStatusInFlightRef.current;
      const sequence = driverStatusSequenceRef.current + 1;
      driverStatusSequenceRef.current = sequence;
      setDriverStatusRefreshing(true);
      const refresh = (async () => {
        try {
          const nextStatus: DriverActivationRequestStatus = await queryDriverActivationStatus(requestID, installationID);
          if (cancelled || !mountedRef.current || driverStatusSequenceRef.current !== sequence) return;
          const nextRequest: DriverActivationRequest = {
            ...activationRequest,
            id: nextStatus.id,
            correlationCode: nextStatus.correlationCode,
            status: nextStatus.authorizationStatus?.toUpperCase() === "REVOKED" ? "REVOKED" : nextStatus.status,
            expiresAt: nextStatus.expiresAt,
            installationId: installationID,
          };
          setActivationRequest(nextRequest);
          void persistDriverActivationRequest(nextRequest).catch(() => undefined);
          const nextState = registrationStateFor(nextStatus.status, nextStatus.authorizationStatus, nextStatus.expiresAt);
          setRegistrationState(nextState);
          switch (nextState) {
            case "pending":
              setStatus("Esperando aprobación del supervisor.");
              break;
            case "approved":
              setStatus("Tu dispositivo fue aprobado. Ya puedes registrar la passkey.");
              break;
            case "registered":
              setStatus("La activación fue completada. Ya puedes ingresar con passkey.");
              break;
            case "revoked":
              setStatus("Este dispositivo fue revocado. Solicita una revisión al supervisor.");
              break;
            case "expired":
              setStatus("La aprobación venció. Solicita una nueva activación");
              break;
            case "rejected":
              setStatus("La solicitud fue rechazada. Puedes iniciar una nueva activación.");
              break;
          }
        } catch (error) {
          if (cancelled || !mountedRef.current || driverStatusSequenceRef.current !== sequence) return;
          if (error instanceof DriverActivationStatusError && (error.status === 401 || error.status === 403)) {
            stopAutomaticPolling = true;
            setStatus(error.status === 401
              ? "La sesión de activación expiró. Ingresa nuevamente para consultar el estado."
              : "No tienes permiso para consultar esta activación.");
          } else if (error instanceof DriverActivationStatusError && error.status === 404) {
            stopAutomaticPolling = true;
            setStatus("No se pudo verificar esta solicitud. Inicia una nueva activación si corresponde.");
          } else {
            setStatus("No se pudo consultar el estado por un problema temporal de red. La solicitud no se considera aprobada.");
          }
        } finally {
          if (driverStatusSequenceRef.current === sequence) {
            driverStatusInFlightRef.current = null;
            setDriverStatusRefreshing(false);
          }
        }
      })();
      driverStatusInFlightRef.current = refresh;
      return refresh;
    };

    driverStatusPollRef.current = poll;
    const intervalID = window.setInterval(() => void poll(), 5000);
    const onVisibilityChange = () => {
      if (document.visibilityState === "visible") void poll();
    };
    document.addEventListener("visibilitychange", onVisibilityChange);
    void poll();
    return () => {
      cancelled = true;
      window.clearInterval(intervalID);
      document.removeEventListener("visibilitychange", onVisibilityChange);
      if (driverStatusPollRef.current === poll) driverStatusPollRef.current = null;
    };
  }, [activationRequest?.id, installation?.id, registrationState]);

  useEffect(() => {
    if (!activationRequest || !["pending", "approved"].includes(registrationState)) return;
    const expire = () => {
      if (registrationStateFor(activationRequest.status, null, activationRequest.expiresAt) === "expired") {
        setRegistrationState("expired");
        setStatus("La aprobación venció. Solicita una nueva activación");
      }
    };
    const remaining = Date.parse(activationRequest.expiresAt) - Date.now();
    const timer = window.setTimeout(expire, Number.isFinite(remaining) ? Math.max(0, Math.min(remaining, 2147483647)) : 0);
    document.addEventListener("visibilitychange", expire);
    return () => { window.clearTimeout(timer); document.removeEventListener("visibilitychange", expire); };
  }, [activationRequest, registrationState]);

  const refreshSupervisor = useCallback(async (source: "manual" | "poll" | "mutation" = "manual"): Promise<void> => {
    if (supervisorRefreshRef.current) {
      if (source !== "mutation") return supervisorRefreshRef.current;
      supervisorAbortRef.current?.abort();
    }
    if (source === "poll" && (document.visibilityState !== "visible" || supervisorActionRef.current !== null || supervisorPollingBlockedRef.current)) return;

    const sequence = supervisorRefreshSequenceRef.current + 1;
    supervisorRefreshSequenceRef.current = sequence;
    const controller = new AbortController();
    supervisorAbortRef.current = controller;
    supervisorRefreshFailedRef.current = false;
    setSupervisorRefreshing(true);
    const refresh = (async () => {
      try {
        const [nextRequests, nextAuthorizations] = await Promise.all([listSupervisorDeviceRequests(controller.signal), listSupervisorDeviceAuthorizations(controller.signal)]);
        if (!mountedRef.current || supervisorRefreshSequenceRef.current !== sequence) return;

        const newRequestIDs = nextRequests
          .map((item) => item.request_id)
          .filter((requestID) => requestID && !knownRequestIDsRef.current.has(requestID));
        for (const requestID of nextRequests.map((item) => item.request_id).filter(Boolean)) knownRequestIDsRef.current.add(requestID);
        if (supervisorSnapshotReadyRef.current && newRequestIDs.length > 0) {
          setSupervisorNotice(newRequestIDs.length === 1 ? "Nueva solicitud de activación pendiente." : `${newRequestIDs.length} nuevas solicitudes de activación pendientes.`);
          if (supervisorNoticeTimerRef.current !== undefined) window.clearTimeout(supervisorNoticeTimerRef.current);
          supervisorNoticeTimerRef.current = window.setTimeout(() => setSupervisorNotice(null), 6000);
        }
        supervisorSnapshotReadyRef.current = true;
        setRequests(nextRequests);
        setAuthorizations(nextAuthorizations);
        supervisorPollingBlockedRef.current = false;
        setSupervisorPollingBlocked(false);
      } catch (error) {
        if (!mountedRef.current || supervisorRefreshSequenceRef.current !== sequence) return;
        supervisorRefreshFailedRef.current = true;
        if (error instanceof SupervisorRequestError && error.status === 401) {
          setSupervisorLoggedIn(false);
          setSupervisorPollingBlocked(true);
          supervisorPollingBlockedRef.current = true;
          setSupervisorStatus("Tu sesión ha expirado. Vuelve a ingresar.");
        } else if (error instanceof SupervisorRequestError && error.status === 403) {
          setSupervisorPollingBlocked(true);
          supervisorPollingBlockedRef.current = true;
          setSupervisorStatus("No tienes permisos para consultar esta consola. Se detuvo la actualización automática.");
        } else {
          setSupervisorStatus("No fue posible actualizar los estados del supervisor.");
        }
      } finally {
        if (supervisorRefreshSequenceRef.current === sequence) {
          supervisorRefreshRef.current = null;
          supervisorAbortRef.current = null;
          setSupervisorRefreshing(false);
        }
      }
    })();
    supervisorRefreshRef.current = refresh;
    return refresh;
  }, []);

  useEffect(() => {
    supervisorActionRef.current = supervisorAction;
  }, [supervisorAction]);

  useEffect(() => {
    if (!supervisorLoggedIn) return;
    const poll = () => {
      if (document.visibilityState === "visible") void refreshSupervisor("poll");
    };
    const intervalID = window.setInterval(poll, 5000);
    const onVisibilityChange = () => {
      if (document.visibilityState === "visible") poll();
    };
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => {
      window.clearInterval(intervalID);
      document.removeEventListener("visibilitychange", onVisibilityChange);
    };
  }, [refreshSupervisor, supervisorLoggedIn]);

  useEffect(() => () => {
    mountedRef.current = false;
    supervisorRefreshSequenceRef.current += 1;
    supervisorAbortRef.current?.abort();
    if (supervisorNoticeTimerRef.current !== undefined) window.clearTimeout(supervisorNoticeTimerRef.current);
  }, []);

  const supervisorLogin = async () => {
    try {
      await loginSupervisor(supervisorIdentifier, supervisorPassword);
      setSupervisorPassword("");
      supervisorSnapshotReadyRef.current = false;
      knownRequestIDsRef.current.clear();
      await refreshSupervisor("manual");
      if (supervisorRefreshFailedRef.current) return;
      setSupervisorLoggedIn(true);
      setSupervisorPollingBlocked(false);
      supervisorPollingBlockedRef.current = false;
      setSupervisorStatus("Sesión activa. Sólo se muestran solicitudes dentro del alcance autorizado.");
    } catch {
      setSupervisorStatus("No fue posible autenticar al supervisor o cargar sus dispositivos.");
    }
  };

  const issueDriverInvitation = async () => {
    if (!selectedInviteDriverID || !companyID) {
      setSupervisorStatus("Selecciona un conductor y una empresa autorizados.");
      return;
    }
    setSupervisorAction("issue-invitation");
    try {
      const issued = await createDriverEnrollmentInvitation({
        person_id: selectedInviteDriverID,
        company_id: companyID,
        ...(operationID ? { company_operation_id: operationID } : {}),
        expires_seconds: 900,
      });
      const link = `${window.location.origin}${window.location.pathname}#driver-invite=${issued.token}`;
      setIssuedInviteLink(link);
      setIssuedInviteCode(issued.confirmation_code);
      setSupervisorStatus("Invitación creada. Comparte el enlace y el código por canales separados.");
    } catch {
      setSupervisorStatus("No fue posible crear la invitación; revisa el alcance autorizado o si ya existe una activa.");
    } finally {
      updateSupervisorAction(null);
    }
  };

  const shareIssuedInvitationByWhatsApp = () => {
    if (!issuedInviteLink) return;
    const message = encodeURIComponent(`Abre este enlace para iniciar tu activación en Transportation Platform: ${issuedInviteLink}`);
    window.open(`https://wa.me/?text=${message}`, "_blank", "noopener,noreferrer");
  };

  const consumeDriverInvitationRequest = async () => {
    if (!driverInviteToken || !driverInvitePreview || !driverInviteCode.trim()) {
      setDriverInviteStatus("Ingresa el código de confirmación entregado por el supervisor.");
      return;
    }
    setDriverInviteBusy(true);
    try {
      const currentInstallation = installation ?? await getOrCreateDriverInstallation();
      if (!installation) {
        setInstallation(currentInstallation);
        setInstallationState("ready");
      }
      const request = await consumeDriverInvitation(driverInviteToken, driverInviteCode.trim(), currentInstallation);
      const associated = await associateDriverInstallationIdentifier(currentInstallation.id, driverInvitePreview.identifier);
      setInstallation(associated);
      setIdentifier(driverInvitePreview.identifier);
      setActivationRequest(request);
      setRegistrationState(registrationStateFor(request.status, null, request.expiresAt));
      setDriverInviteStatus("Activación iniciada. Espera la aprobación del supervisor para registrar tu passkey.");
      await persistDriverActivationRequest(request);
    } catch {
      setDriverInviteStatus("No se pudo iniciar la activación. El enlace puede haber vencido, sido usado o no corresponder a esta instalación.");
    } finally {
      setDriverInviteBusy(false);
    }
  };

  const approveRequest = async (requestID: string) => {
    if (!window.confirm("¿Confirmas aprobar esta instalación? El conductor podrá registrar su passkey.")) return;
    updateSupervisorAction(`approve:${requestID}`);
    try {
      await approveSupervisorDeviceRequest(requestID, "Aprobación de demo del cliente");
      await refreshSupervisor("mutation");
      setSupervisorStatus("Solicitud aprobada. El conductor ya puede registrar su passkey.");
    } catch {
      setSupervisorStatus("La solicitud no pudo aprobarse; su estado o alcance puede haber cambiado.");
    } finally {
      updateSupervisorAction(null);
    }
  };

  const revokeAuthorization = async (authorizationID: string) => {
    if (!window.confirm("¿Confirmas revocar este dispositivo? El acceso del conductor se bloqueará inmediatamente.")) return;
    updateSupervisorAction(`revoke:${authorizationID}`);
    try {
      await revokeSupervisorDevice(authorizationID, "Revocación de demo del cliente");
      await refreshSupervisor("mutation");
      setSupervisorStatus("Dispositivo revocado. El próximo acceso del conductor será rechazado.");
    } catch {
      setSupervisorStatus("El dispositivo no pudo revocarse; su estado o alcance puede haber cambiado.");
    } finally {
      updateSupervisorAction(null);
    }
  };

  const prepareInstallation = async () => {
    setDriverBusy("prepare");
    try {
      const existing = await getOrCreateDriverInstallation();
      setInstallation(existing);
      setInstallationState("ready");
      setStatus("Instalación preparada o recuperada desde este navegador. Nunca se guardan tokens de sesión aquí.");
    } catch {
      setInstallationState("error");
      setStatus("No fue posible preparar la instalación en este navegador.");
    } finally {
      setDriverBusy(null);
    }
  };

  const changeDriverIdentity = async () => {
    if (!installation || !window.confirm("¿Confirmas cambiar el conductor asociado a esta instalación? Deberás iniciar una nueva activación; la clave local no se exportará.")) return;
    setDriverBusy("prepare");
    try {
      const cleared = await clearDriverInstallationIdentifier(installation.id);
      await clearDriverActivationRequest(installation.id);
      setInstallation(cleared);
      setActivationRequest(undefined);
      setRegistrationState("idle");
      setIdentifier("");
      setStatus("Identidad local eliminada. Prepara una nueva activación con el conductor autorizado.");
    } catch {
      setStatus("No se pudo cambiar la identidad local de esta instalación.");
    } finally {
      setDriverBusy(null);
    }
  };

  const activate = async () => {
    if (!identifier.trim() || !password || !installation || !companyID) {
      setStatus("Completa tu identificación, contraseña, empresa y prepara la instalación.");
      return;
    }
    setDriverBusy("activate");
    try {
      await loginDriverWithPassword(identifier, password);
       const request = await requestDriverActivation(companyID, operationID || undefined, installation, identifier);
      setActivationRequest(request);
      setRegistrationState("pending");
      setPassword("");
      setStatus("Solicitud creada. Espera la aprobación del supervisor antes de registrar la passkey.");
    } catch (error) {
      setStatus(error instanceof DriverActivationRequestConflictError
        ? "Ya existe una solicitud de activación vigente para este dispositivo. Continúa con esa solicitud o consulta su estado."
        : "La activación fue rechazada. La respuesta no revela si el conductor existe.");
    } finally {
      setDriverBusy(null);
    }
  };

  const registerApprovedPasskey = async () => {
    if (!installation || !activationRequest) {
      setStatus("Solicita primero la activación y espera la aprobación del supervisor.");
      return;
    }
    if (registrationState !== "approved") {
      setStatus("Espera la aprobación del supervisor antes de registrar la passkey.");
      return;
    }
    if (Date.parse(activationRequest.expiresAt) <= Date.now()) {
      setRegistrationState("expired");
      setStatus("La solicitud expiró. Solicita una nueva; tu instalación local se conserva.");
      return;
    }
    setDriverBusy("register");
    setRegistrationState("registering");
    setStatus("Verificando aprobación. Confirma la biometría o el PIN del dispositivo.");
    try {
      await registerPasskey("", activationRequest.id, (stage) => {
        if (stage === "options_received") {
          setRegistrationState("approved");
          setStatus("Solicitud aprobada. Confirma ahora la biometría o el PIN.");
        }
      });
      const candidateIdentifier = activationRequest.identifier ?? identifier.trim().toLowerCase();
      if (candidateIdentifier && !installation.identifier) {
        try {
          const associated = await associateDriverInstallationIdentifier(installation.id, candidateIdentifier);
          setInstallation(associated);
          setIdentifier(candidateIdentifier);
        } catch {
          setStatus("Passkey registrada. No se pudo guardar la identidad local; usa tu identificador una vez para el próximo ingreso.");
        }
      }
      void clearDriverActivationRequest(installation.id).catch(() => undefined);
      setRegistrationState("registered");
      if (!candidateIdentifier || installation.identifier) setStatus("Passkey registrada y dispositivo autorizado. Ya puedes ingresar con passkey.");
    } catch (error) {
      if (Date.parse(activationRequest.expiresAt) <= Date.now()) {
        setRegistrationState("expired");
        setStatus("La solicitud expiró. Solicita una nueva; tu instalación local se conserva.");
      } else if (error instanceof Error && error.name === "NotAllowedError") {
        setRegistrationState("pending");
        setStatus("Registro cancelado. La solicitud sigue disponible mientras no expire.");
      } else {
        setRegistrationState("pending");
        setStatus("La solicitud sigue pendiente o fue rechazada. Verifica con el supervisor y reintenta.");
      }
    } finally {
      setDriverBusy(null);
    }
  };

  const recurringLogin = async () => {
    if (!installation) {
      setStatus("Prepara primero la instalación en este navegador.");
      return;
    }
    setDriverBusy("login");
    try {
      const effectiveIdentifier = installation.identifier ?? identifier.trim().toLowerCase();
      if (!effectiveIdentifier) {
        setStatus("Esta instalación no tiene una identidad local. Escribe tu identificador para el primer ingreso.");
        return;
      }
      await loginDriverWithPasskey(effectiveIdentifier, installation);
      if (!installation.identifier) {
        try {
          const associated = await associateDriverInstallationIdentifier(installation.id, effectiveIdentifier);
          setInstallation(associated);
          setIdentifier(effectiveIdentifier);
        } catch {
          setStatus("Acceso concedido. No se pudo guardar la identidad local; vuelve a usar tu identificador la próxima vez.");
          return;
        }
      }
      setStatus("Acceso concedido. La passkey y la prueba de posesión validaron esta instalación.");
    } catch (error) {
      setStatus(driverLoginErrorMessage(error));
    } finally {
      setDriverBusy(null);
    }
  };

  return <main className="min-h-screen bg-slate-950 px-4 py-5 text-slate-950 sm:px-8 sm:py-10">
    <div className="mx-auto max-w-6xl">
      <header className="overflow-hidden rounded-[2rem] bg-white shadow-2xl shadow-slate-950/30">
        <div className="bg-gradient-to-br from-teal-950 via-slate-900 to-violet-950 px-6 py-8 text-white sm:px-10 sm:py-12">
          <div className="flex flex-col gap-8 lg:flex-row lg:items-end lg:justify-between">
            <div><p className="text-xs font-black uppercase tracking-[0.24em] text-teal-300">Transportation Platform</p><h1 className="mt-3 max-w-3xl text-4xl font-black tracking-tight sm:text-6xl">Acceso seguro para cada operación</h1><p className="mt-5 max-w-2xl text-base leading-7 text-slate-200">Una experiencia sencilla para conductores y supervisores, respaldada por passkeys y aprobación controlada.</p></div>
            <div className="rounded-2xl border border-white/15 bg-white/10 px-5 py-4 text-sm leading-6 text-slate-200 backdrop-blur"><p className="font-bold text-white">Demo controlada</p><p>No hay formularios operativos, GPS ni modo offline en esta fase.</p></div>
          </div>
        </div>
        <div className="grid gap-px bg-slate-200 sm:grid-cols-3">
          <div className="bg-white px-6 py-5"><p className="text-xs font-black uppercase tracking-[0.16em] text-teal-700">Acceso</p><p className="mt-2 font-bold text-slate-950">Passkey del dispositivo</p></div>
          <div className="bg-white px-6 py-5"><p className="text-xs font-black uppercase tracking-[0.16em] text-amber-700">Control</p><p className="mt-2 font-bold text-slate-950">Aprobación supervisada</p></div>
          <div className="bg-white px-6 py-5"><p className="text-xs font-black uppercase tracking-[0.16em] text-violet-700">Respuesta</p><p className="mt-2 font-bold text-slate-950">Revocación inmediata</p></div>
        </div>
      </header>

      {driverInviteToken && <section className="mt-6 rounded-[2rem] border-2 border-violet-200 bg-violet-50 p-5 shadow-xl sm:p-8" aria-label="Invitación de activación de conductor">
        <p className="text-xs font-black uppercase tracking-[0.18em] text-violet-700">Enrolamiento remoto</p>
        <h2 className="mt-2 text-2xl font-black text-slate-950">Invitación para activar tu dispositivo</h2>
        {driverInvitePreview ? <>
          <p className="mt-3 text-sm leading-6 text-slate-700">Conductor: <strong>{driverInvitePreview.legal_name || driverInvitePreview.identifier}</strong>. Empresa: <strong>{driverInvitePreview.company_name}</strong>. Operación: <strong>{driverInvitePreview.operation_name || "Alcance empresarial"}</strong>.</p>
          <p className="mt-2 text-xs text-slate-600">El enlace por sí solo no autoriza el acceso. Ingresa el código de confirmación entregado por el supervisor y prepara esta instalación.</p>
          <div className="mt-5 grid gap-4 sm:grid-cols-[1fr_auto]"><label className="text-sm font-semibold text-slate-700">Código de confirmación<input className="mt-2 w-full rounded-xl border border-violet-300 bg-white p-3.5 tracking-[0.3em] outline-none focus:border-violet-700 focus:ring-4 focus:ring-violet-100" value={driverInviteCode} onChange={(event) => setDriverInviteCode(event.target.value.replace(/\D/g, "").slice(0, 8))} inputMode="numeric" autoComplete="one-time-code" placeholder="8 dígitos" /></label><button className="self-end rounded-xl bg-violet-700 px-4 py-3.5 text-sm font-bold text-white hover:bg-violet-800 disabled:cursor-not-allowed disabled:opacity-50" disabled={driverInviteBusy || driverInviteCode.length !== 8} onClick={() => void consumeDriverInvitationRequest()}>{driverInviteBusy ? "Validando…" : "Iniciar activación"}</button></div>
        </> : <p className="mt-4 rounded-xl bg-white p-4 text-sm text-violet-900" role="status">{driverInviteStatus}</p>}
        {driverInviteStatus && driverInvitePreview && <p className="mt-4 rounded-xl bg-white p-4 text-sm text-violet-900" role="status">{driverInviteStatus}</p>}
      </section>}

      <section className="mt-6 rounded-[2rem] bg-white p-5 shadow-2xl shadow-slate-950/20 sm:p-8" aria-label="Acceso móvil de conductor">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between"><div><p className="text-xs font-black uppercase tracking-[0.18em] text-teal-700">Vista de conductor</p><h2 className="mt-2 text-2xl font-black text-slate-950 sm:text-3xl">{recognizedDriver ? "Bienvenido de nuevo" : "Activar o ingresar con passkey"}</h2><p className="mt-2 max-w-2xl text-sm leading-6 text-slate-600">{recognizedDriver ? "Esta instalación ya está asociada a tu identidad. Ingresa con passkey para continuar." : "Usa tu identidad una sola vez para habilitar este dispositivo. Después, la passkey será tu forma habitual de ingreso."}</p></div><div className={["rounded-2xl border px-4 py-3 text-sm", installationState === "ready" ? "border-teal-200 bg-teal-50 text-teal-900" : "border-slate-200 bg-slate-50 text-slate-600"].join(" ")}><p className="font-bold">{installationState === "ready" ? "Dispositivo preparado" : "Dispositivo sin activar"}</p><p className="mt-1">{installationState === "ready" ? "Instalación recuperada o lista para usar." : "Prepara la instalación para comenzar."}</p></div></div>
        <div className="mt-6"><StepList registrationState={registrationState} installationState={installationState} /></div>
        <div className="mt-7 grid gap-6 lg:grid-cols-[1.1fr_0.9fr]">
          <div className="space-y-4">
            <div className="grid gap-4 sm:grid-cols-2"><label className="text-sm font-semibold text-slate-700">Identificación del conductor<input className="mt-2 w-full rounded-xl border border-slate-300 bg-white p-3.5 outline-none transition focus:border-teal-600 focus:ring-4 focus:ring-teal-100 disabled:bg-slate-100" value={identifier} onChange={(event) => setIdentifier(event.target.value)} autoComplete="username" placeholder="Tu usuario de demo" disabled={recognizedDriver} /></label><label className="text-sm font-semibold text-slate-700">Contraseña de activación{recognizedDriver && <span className="font-normal text-slate-500"> (sólo fallback)</span>}<input className="mt-2 w-full rounded-xl border border-slate-300 bg-white p-3.5 outline-none transition focus:border-teal-600 focus:ring-4 focus:ring-teal-100" value={password} onChange={(event) => setPassword(event.target.value)} type="password" autoComplete="current-password" placeholder="Sólo para la activación inicial" /></label></div>
            {installation?.identifier ? <div className="rounded-xl border border-teal-200 bg-teal-50 p-3 text-sm text-teal-900"><p>Conductor asociado a esta instalación: <strong>{installation.identifier}</strong></p><button className="mt-2 rounded-lg border border-teal-300 bg-white px-3 py-2 text-xs font-bold text-teal-900 hover:bg-teal-100 disabled:cursor-not-allowed disabled:opacity-50" disabled={driverBusy !== null} onClick={() => void changeDriverIdentity()}>Cambiar conductor</button></div> : <p className="text-xs leading-5 text-slate-500">La identidad se guarda localmente sólo después de un enrolamiento o ingreso validado. Las instalaciones antiguas pueden usar este campo una vez como fallback.</p>}
            <div className="grid gap-4 sm:grid-cols-2"><label className="text-sm font-semibold text-slate-700">Empresa<select className="mt-2 w-full rounded-xl border border-slate-300 bg-white p-3.5 outline-none transition focus:border-teal-600 focus:ring-4 focus:ring-teal-100" value={companyID} onChange={(event) => { setCompanyID(event.target.value); setOperationID(""); }}><option value="">Selecciona una empresa</option>{demoCompanies.map((company) => <option key={company.id} value={company.id}>{company.name}</option>)}</select></label><label className="text-sm font-semibold text-slate-700">Operación <span className="font-normal text-slate-500">(opcional)</span><select className="mt-2 w-full rounded-xl border border-slate-300 bg-white p-3.5 outline-none transition focus:border-teal-600 focus:ring-4 focus:ring-teal-100" value={operationID} onChange={(event) => setOperationID(event.target.value)} disabled={!selectedCompany}><option value="">Acceso por empresa</option>{selectedCompany?.operations.map((operation) => <option key={operation.id} value={operation.id}>{operation.name}</option>)}</select></label></div>
            <p className="text-xs leading-5 text-slate-500">La demo muestra sólo datos sintéticos autorizados. La operación puede dejarse vacía para una solicitud de alcance empresarial.</p>
          </div>
           <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 sm:p-5"><p className="text-sm font-black text-slate-950">{recognizedDriver ? "Acceso recurrente" : "¿Qué quieres hacer?"}</p><div className="mt-4 grid gap-3">{recognizedDriver && <button className="rounded-xl bg-violet-700 px-4 py-3.5 text-sm font-bold text-white transition hover:bg-violet-800 disabled:cursor-not-allowed disabled:opacity-50" disabled={driverBusy !== null} onClick={() => void recurringLogin()}>{driverBusy === "login" ? "Verificando…" : "Ingresar con passkey"}</button>}<button className="rounded-xl bg-slate-900 px-4 py-3.5 text-sm font-bold text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50" disabled={driverBusy !== null} onClick={() => void prepareInstallation()}>{driverBusy === "prepare" ? "Preparando…" : installationState === "ready" ? "Usar esta instalación" : "Preparar instalación"}</button><button className={["rounded-xl px-4 py-3.5 text-sm font-bold text-white transition disabled:cursor-not-allowed disabled:opacity-50", recognizedDriver ? "border border-teal-700 bg-white text-teal-800 hover:bg-teal-50" : "bg-teal-700 hover:bg-teal-800"].join(" ")} disabled={driverBusy !== null} onClick={() => void activate()}>{driverBusy === "activate" ? "Enviando solicitud…" : recognizedDriver || registrationState === "expired" ? "Solicitar nueva activación" : "Activar este dispositivo"}</button><button className="rounded-xl bg-amber-700 px-4 py-3.5 text-sm font-bold text-white transition hover:bg-amber-800 disabled:cursor-not-allowed disabled:opacity-50" disabled={!activationRequest || registrationState !== "approved" || driverBusy !== null} onClick={() => void registerApprovedPasskey()}>{driverBusy === "register" ? "Registrando…" : registrationState === "registered" ? "Passkey registrada" : "Registrar passkey"}</button>{!recognizedDriver && <button className="rounded-xl bg-violet-700 px-4 py-3.5 text-sm font-bold text-white transition hover:bg-violet-800 disabled:cursor-not-allowed disabled:opacity-50" disabled={driverBusy !== null} onClick={() => void recurringLogin()}>{driverBusy === "login" ? "Verificando…" : "Ingresar con passkey"}</button>}</div></div>
        </div>
        {activationRequest && <div className="mt-5 rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-950" aria-live="polite"><p className="font-bold">{registrationState === "pending" && "Esperando aprobación del supervisor"}{registrationState === "approved" && "Tu dispositivo fue aprobado"}{registrationState === "registering" && "Registro en curso"}{registrationState === "registered" && "Dispositivo autorizado"}{registrationState === "expired" && "La aprobación venció. Solicita una nueva activación"}{registrationState === "rejected" && "La solicitud fue rechazada"}{registrationState === "revoked" && "Este dispositivo fue revocado"}</p><p className="mt-1">{registrationState === "pending" && `Expira el ${formatDate(activationRequest.expiresAt)}. La pantalla consultará el estado mientras esté visible.`}{registrationState === "approved" && "Ya puedes registrar la passkey con la interacción explícita del conductor."}{registrationState === "registering" && "No cierres esta pantalla mientras se completa la verificación."}{registrationState === "registered" && "Ya puedes usar Ingresar con passkey en este dispositivo."}{registrationState === "expired" && "Conservamos tu instalación y su clave de seguridad. Usa Solicitar nueva activación después de volver a identificarte."}{registrationState === "rejected" && "Puedes iniciar una nueva activación mediante el proceso autorizado."}{registrationState === "revoked" && "No se ofrecerá una reactivación automática."}</p>{registrationState === "pending" && <button className="mt-3 rounded-xl border border-amber-300 bg-white px-3 py-2 text-xs font-bold text-amber-900 disabled:cursor-not-allowed disabled:opacity-50" disabled={driverStatusRefreshing} onClick={() => void driverStatusPollRef.current?.(true)}>{driverStatusRefreshing ? "Consultando…" : "Consultar estado"}</button>}<details className="mt-3 text-xs text-amber-900"><summary className="cursor-pointer font-bold">Detalles técnicos</summary><dl className="mt-2 grid gap-1 sm:grid-cols-2"><div><dt className="font-semibold">Código</dt><dd className="break-all">{activationRequest.correlationCode || "—"}</dd></div><div><dt className="font-semibold">Solicitud</dt><dd className="break-all">{activationRequest.id}</dd></div></dl></details></div>}
        <p className="mt-4 rounded-2xl bg-slate-100 p-4 text-sm text-slate-700" role="status">{status}</p>
      </section>

      <section className="mt-6 rounded-[2rem] bg-white p-5 shadow-2xl shadow-slate-950/20 sm:p-8" aria-label="Módulos de negocio"><p className="text-xs font-black uppercase tracking-[0.18em] text-slate-500">Una plataforma que puede crecer contigo</p><h2 className="mt-2 text-2xl font-black text-slate-950">Operaciones conectadas</h2><p className="mt-2 max-w-2xl text-sm leading-6 text-slate-600">Cada tipo de operación conserva sus propias reglas, mientras la identidad y la seguridad se comparten de forma controlada.</p><div className="mt-6 grid gap-4 md:grid-cols-3">{modules.map((module) => <ModuleCard key={module.name} {...module} />)}</div></section>

      <section className="mt-6 rounded-[2rem] bg-white p-5 shadow-2xl shadow-slate-950/20 sm:p-8" aria-label="Consola de supervisor">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between"><div><p className="text-xs font-black uppercase tracking-[0.18em] text-amber-700">Vista de supervisor</p><h2 className="mt-2 text-2xl font-black text-slate-950 sm:text-3xl">Control de dispositivos</h2><p className="mt-2 max-w-3xl text-sm leading-6 text-slate-600">Revisa solicitudes, aprueba nuevas instalaciones y revoca accesos sin salir de esta consola.</p></div>{supervisorLoggedIn && <button className="rounded-xl border border-slate-300 px-4 py-2.5 text-sm font-bold text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50" disabled={supervisorRefreshing || supervisorAction !== null} onClick={() => void refreshSupervisor()}>{supervisorRefreshing ? "Actualizando…" : "Actualizar ahora"}</button>}</div>
        <div className="mt-6 grid gap-4 md:grid-cols-[1fr_1fr_auto]"><label className="text-sm font-semibold text-slate-700">Usuario supervisor<input className="mt-2 w-full rounded-xl border border-slate-300 bg-white p-3.5 outline-none transition focus:border-amber-600 focus:ring-4 focus:ring-amber-100" value={supervisorIdentifier} onChange={(event) => setSupervisorIdentifier(event.target.value)} autoComplete="username" placeholder="Usuario de demo" /></label><label className="text-sm font-semibold text-slate-700">Contraseña<input className="mt-2 w-full rounded-xl border border-slate-300 bg-white p-3.5 outline-none transition focus:border-amber-600 focus:ring-4 focus:ring-amber-100" value={supervisorPassword} onChange={(event) => setSupervisorPassword(event.target.value)} type="password" autoComplete="current-password" placeholder="Contraseña de demo" /></label><button className="self-end rounded-xl bg-slate-900 px-4 py-3.5 text-sm font-bold text-white hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50" disabled={supervisorRefreshing || supervisorAction !== null} onClick={() => void supervisorLogin()}>{supervisorRefreshing ? "Cargando…" : "Ingresar supervisor"}</button></div>
        <p className="mt-4 rounded-2xl bg-slate-100 p-4 text-sm text-slate-700" aria-live="polite">{supervisorStatus}</p>
        {supervisorLoggedIn && <fieldset className="mt-4 grid gap-3 sm:grid-cols-2"><legend className="font-bold text-slate-800">Alcance de gestión e invitaciones</legend>
          <label className="text-sm font-semibold">Empresa del supervisor<select className="mt-1 w-full rounded-xl border border-slate-300 p-3" value={companyID} disabled={supervisorAction !== null} onChange={(event) => { setCompanyID(event.target.value); setOperationID(""); }}>
            {demoCompanies.map((company) => <option key={company.id} value={company.id}>{company.name}</option>)}
          </select></label>
          <label className="text-sm font-semibold">Operación para invitaciones<select className="mt-1 w-full rounded-xl border border-slate-300 p-3" value={operationID} disabled={supervisorAction !== null} onChange={(event) => setOperationID(event.target.value)}><option value="">Acceso por empresa</option>{selectedCompany?.operations.map((operation) => <option key={operation.id} value={operation.id}>{operation.name}</option>)}</select></label>
        </fieldset>}
        {supervisorLoggedIn && <div className="mt-5 rounded-2xl border border-violet-200 bg-violet-50 p-4"><div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"><div><h3 className="font-black text-violet-950">Activación remota por WhatsApp</h3><p className="mt-1 text-xs leading-5 text-violet-900">Genera un enlace de un solo uso y comparte el código de confirmación por separado. La plataforma no envía WhatsApp.</p></div><button className="rounded-xl bg-violet-700 px-4 py-2.5 text-sm font-bold text-white hover:bg-violet-800 disabled:cursor-not-allowed disabled:opacity-50" disabled={supervisorAction !== null || supervisorCandidates.length === 0} onClick={() => void issueDriverInvitation()}>Generar invitación</button></div>{candidateError && <p className="mt-3 text-sm text-rose-800" role="alert">{candidateError}</p>}<div className="mt-4 grid gap-4 sm:grid-cols-2"><label className="text-sm font-semibold text-violet-950">Conductor<select className="mt-2 w-full rounded-xl border border-violet-300 bg-white p-3" value={selectedInviteDriverID} onChange={(event) => setSelectedInviteDriverID(event.target.value)}><option value="">Selecciona un conductor</option>{supervisorCandidates.map((candidate) => <option key={candidate.person_id} value={candidate.person_id}>{candidate.legal_name || candidate.identifier || candidate.person_id}</option>)}</select></label><div className="rounded-xl bg-white p-3 text-sm text-violet-950"><p className="font-semibold">Alcance</p><p className="mt-1">{companyName(companyID)} · {operationName(operationID || null)}</p></div></div>{issuedInviteLink && <div className="mt-4 grid gap-3 rounded-xl bg-white p-3 text-sm text-violet-950"><p className="font-semibold">Enlace para compartir manualmente</p><code className="break-all text-xs">{issuedInviteLink}</code><div className="flex flex-wrap items-center gap-3"><p><span className="font-semibold">Código separado:</span> <code className="tracking-[0.3em]">{issuedInviteCode}</code></p><button className="rounded-lg border border-violet-300 bg-white px-3 py-2 text-xs font-bold text-violet-800 hover:bg-violet-50" onClick={shareIssuedInvitationByWhatsApp}>Compartir por WhatsApp</button></div><p className="text-xs">El token sólo se muestra en esta respuesta y no se guarda en el navegador. El código se entrega por separado.</p></div>}</div>}
        {supervisorNotice && <div className="mt-3 rounded-2xl border border-teal-200 bg-teal-50 p-4 text-sm font-semibold text-teal-900" role="status" aria-live="polite">{supervisorNotice}</div>}
        {supervisorPollingBlocked && supervisorLoggedIn && <p className="mt-3 rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">La actualización automática está detenida por permisos. Puedes intentar «Actualizar ahora» después de corregir el acceso.</p>}
        {supervisorLoggedIn && <DriverManagement key={companyID} companyID={companyID} operations={selectedCompany?.operations ?? []}
          onChanged={() => setCandidateRevision((value) => value + 1)}
          canInvite={(personID) => supervisorCandidates.some((candidate) => candidate.person_id === personID)}
          onInvite={(personID) => { setSelectedInviteDriverID(personID); setSupervisorStatus("Conductor seleccionado. Usa Generar invitación para confirmar el envío."); }} />}
        {supervisorLoggedIn && <div className="mt-6">
          <div className="grid gap-3 sm:grid-cols-4">
            <div className="rounded-2xl border border-amber-200 bg-amber-50 p-4"><p className="text-xs font-black uppercase tracking-wide text-amber-800">Pendientes</p><p className="mt-1 text-3xl font-black text-amber-950">{pendingRequests.length}</p></div>
            <div className="rounded-2xl border border-sky-200 bg-sky-50 p-4"><p className="text-xs font-black uppercase tracking-wide text-sky-800">Aprobadas esperando passkey</p><p className="mt-1 text-3xl font-black text-sky-950">{approvedRequests.length}</p></div>
            <div className="rounded-2xl border border-emerald-200 bg-emerald-50 p-4"><p className="text-xs font-black uppercase tracking-wide text-emerald-800">Registrados activos</p><p className="mt-1 text-3xl font-black text-emerald-950">{activeAuthorizations.length}</p></div>
            <div className="rounded-2xl border border-slate-200 bg-slate-100 p-4"><p className="text-xs font-black uppercase tracking-wide text-slate-600">Historial revocado</p><p className="mt-1 text-3xl font-black text-slate-900">{revokedAuthorizations.length}</p></div>
          </div>
          <div className="mt-6 grid gap-6 lg:grid-cols-2">
            <div>
              <h3 className="text-lg font-black text-slate-950">Solicitudes pendientes</h3>
              <div className="mt-3 space-y-3">
                {pendingRequests.length === 0 && <p className="rounded-2xl border border-dashed border-slate-300 p-5 text-sm text-slate-500">No hay solicitudes pendientes visibles en tu alcance.</p>}
                {pendingRequests.map((item) => <article className="rounded-2xl border border-amber-200 bg-amber-50 p-4" key={item.request_id}>
                  <div className="flex items-start justify-between gap-3"><div><p className="font-bold text-slate-950">{item.legal_name || "Conductor de demo"}</p><p className="text-sm text-slate-600">{item.identifier || "Identidad verificada"}</p></div><span className="rounded-full bg-white px-3 py-1 text-xs font-bold text-amber-800">{statusLabel(item.status)}</span></div>
                  <dl className="mt-4 grid gap-2 text-sm text-slate-700 sm:grid-cols-2"><div><dt className="text-xs font-semibold uppercase tracking-wide text-slate-500">Empresa</dt><dd>{companyName(item.company_id)}</dd></div><div><dt className="text-xs font-semibold uppercase tracking-wide text-slate-500">Operación</dt><dd>{operationName(item.company_operation_id)}</dd></div><div><dt className="text-xs font-semibold uppercase tracking-wide text-slate-500">Solicitada</dt><dd>{formatDate(item.requested_at)}</dd></div><div><dt className="text-xs font-semibold uppercase tracking-wide text-slate-500">Expira</dt><dd>{formatDate(item.expires_at)}</dd></div></dl>
                  <button className="mt-4 w-full rounded-xl bg-amber-700 px-3 py-3 text-sm font-bold text-white hover:bg-amber-800 disabled:cursor-not-allowed disabled:opacity-50" disabled={!item.request_id || supervisorAction !== null} onClick={() => void approveRequest(item.request_id)}>{supervisorAction === `approve:${item.request_id}` ? "Aprobando…" : "Aprobar instalación"}</button>
                  <details className="mt-3 text-xs text-slate-600"><summary className="cursor-pointer font-bold">Detalles técnicos</summary><dl className="mt-2 grid gap-1"><div><dt className="font-semibold">Solicitud</dt><dd className="break-all">{item.request_id}</dd></div><div><dt className="font-semibold">Código</dt><dd>{item.correlation_code || "—"}</dd></div><div><dt className="font-semibold">Instalación</dt><dd className="break-all">{item.installation_id}</dd></div><div><dt className="font-semibold">Empresa / operación</dt><dd className="break-all">{item.company_id} / {item.company_operation_id ?? "NULL"}</dd></div></dl></details>
                </article>)}
              </div>
              {approvedRequests.length > 0 && <div className="mt-8"><h3 className="text-lg font-black text-slate-950">Aprobadas — esperando passkey</h3><div className="mt-3 space-y-3">{approvedRequests.map((item) => <article className="rounded-2xl border border-sky-200 bg-sky-50 p-4" key={item.request_id}><div className="flex items-start justify-between gap-3"><div><p className="font-bold text-slate-950">{item.legal_name || "Conductor de demo"}</p><p className="text-sm text-slate-600">{item.identifier || "Identidad verificada"}</p></div><span className="rounded-full bg-white px-3 py-1 text-xs font-bold text-sky-800">Aprobado — esperando passkey</span></div><dl className="mt-4 grid gap-2 text-sm text-slate-700 sm:grid-cols-2"><div><dt className="text-xs font-semibold uppercase tracking-wide text-slate-500">Empresa</dt><dd>{companyName(item.company_id)}</dd></div><div><dt className="text-xs font-semibold uppercase tracking-wide text-slate-500">Operación</dt><dd>{operationName(item.company_operation_id)}</dd></div><div><dt className="text-xs font-semibold uppercase tracking-wide text-slate-500">Aprobada</dt><dd>{formatDate(item.approved_at)}</dd></div><div><dt className="text-xs font-semibold uppercase tracking-wide text-slate-500">Expira</dt><dd>{formatDate(item.expires_at)}</dd></div></dl><details className="mt-3 text-xs text-slate-600"><summary className="cursor-pointer font-bold">Detalles técnicos</summary><dl className="mt-2 grid gap-1"><div><dt className="font-semibold">Solicitud</dt><dd className="break-all">{item.request_id}</dd></div><div><dt className="font-semibold">Instalación</dt><dd className="break-all">{item.installation_id}</dd></div></dl></details></article>)}</div></div>}
              {closedRequests.length > 0 && <div className="mt-8"><h3 className="text-lg font-black text-slate-950">Solicitudes cerradas</h3><div className="mt-3 space-y-3">{closedRequests.map((item) => <article className="rounded-2xl border border-slate-200 bg-slate-50 p-4" key={item.request_id}><div className="flex items-start justify-between gap-3"><div><p className="font-bold text-slate-950">{item.legal_name || "Conductor de demo"}</p><p className="text-sm text-slate-600">{item.identifier || "Identidad verificada"}</p></div><span className="rounded-full bg-white px-3 py-1 text-xs font-bold text-slate-700">{statusLabel(effectiveRequestStatus(item))}</span></div><p className="mt-3 text-sm text-slate-600">Esta solicitud ya no puede aprobarse.</p></article>)}</div></div>}
            </div>
            <div><h3 className="text-lg font-black text-slate-950">Dispositivos registrados</h3><div className="mt-3 space-y-3">{authorizations.length === 0 && <p className="rounded-2xl border border-dashed border-slate-300 p-5 text-sm text-slate-500">Aún no hay dispositivos registrados.</p>}{authorizations.map((item) => <article className={["rounded-2xl", "border", "p-4", item.status.toUpperCase() === "REVOKED" ? "border-slate-300 bg-slate-100" : "border-emerald-200 bg-emerald-50"].join(" ")} key={item.authorization_id}><div className="flex items-start justify-between gap-3"><div><p className="font-bold text-slate-950">{item.legal_name || "Conductor de demo"}</p><p className="text-sm text-slate-600">{item.identifier || "Identidad verificada"}</p></div><span className="rounded-full bg-white px-3 py-1 text-xs font-bold text-slate-700">{statusLabel(item.status)}</span></div><dl className="mt-4 grid gap-2 text-sm text-slate-700 sm:grid-cols-2"><div><dt className="text-xs font-semibold uppercase tracking-wide text-slate-500">Empresa</dt><dd>{companyName(item.company_id)}</dd></div><div><dt className="text-xs font-semibold uppercase tracking-wide text-slate-500">Operación</dt><dd>{operationName(item.company_operation_id)}</dd></div><div><dt className="text-xs font-semibold uppercase tracking-wide text-slate-500">Autorizado</dt><dd>{formatDate(item.approved_at)}</dd></div><div><dt className="text-xs font-semibold uppercase tracking-wide text-slate-500">Revocado</dt><dd>{formatDate(item.revoked_at)}</dd></div></dl>{item.status.toUpperCase() === "ACTIVE" && <button className="mt-4 w-full rounded-xl bg-rose-700 px-3 py-3 text-sm font-bold text-white hover:bg-rose-800 disabled:cursor-not-allowed disabled:opacity-50" disabled={!item.authorization_id || supervisorAction !== null} onClick={() => void revokeAuthorization(item.authorization_id)}>{supervisorAction === `revoke:${item.authorization_id}` ? "Revocando…" : "Revocar dispositivo"}</button>}<details className="mt-3 text-xs text-slate-600"><summary className="cursor-pointer font-bold">Detalles técnicos</summary><dl className="mt-2 grid gap-1"><div><dt className="font-semibold">Autorización</dt><dd className="break-all">{item.authorization_id}</dd></div><div><dt className="font-semibold">Instalación</dt><dd className="break-all">{item.installation_id}</dd></div><div><dt className="font-semibold">Empresa / operación</dt><dd className="break-all">{item.company_id} / {item.company_operation_id ?? "NULL"}</dd></div><div><dt className="font-semibold">Algoritmo</dt><dd>{item.key_algorithm || "—"}</dd></div></dl></details></article>)}</div></div>
          </div>
        </div>}
      </section>
    </div>
  </main>;
}
