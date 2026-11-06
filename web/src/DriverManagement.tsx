import { useEffect, useRef, useState } from "react";
import { request, SupervisorRequestError } from "./supervisor";
import type { DemoOperation } from "./demoCatalog";

type Driver = {
  person_id: string; identifier: string; legal_name: string; phone: string;
  status: string; version: number; operation_ids: string[]; enrollment_status: string;
};
type Result = { status: string; drivers?: Driver[] };
type Props = { companyID: string; operations: DemoOperation[]; onChanged: () => void; onInvite: (personID: string) => void; canInvite: (personID: string) => boolean };

const inputClass = "mt-1 w-full rounded-xl border border-slate-300 bg-white p-3";
const buttonClass = "rounded-xl border border-teal-700 px-4 py-3 text-sm font-bold text-teal-900 disabled:opacity-50";

export function DriverManagement({ companyID, operations, onChanged, onInvite, canInvite }: Props) {
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [allowed, setAllowed] = useState(false);
  const [search, setSearch] = useState("");
  const [revision, setRevision] = useState(0);
  const [showForm, setShowForm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [form, setForm] = useState({ given_name: "", family_name: "", identifier: "", phone: "", operation_ids: [] as string[] });
  const [selectedOps, setSelectedOps] = useState<Record<string, string[]>>({});
  const retry = useRef<{ payload: string; key: string } | null>(null);
  const generation = useRef(0);
  useEffect(() => {
    const controller = new AbortController();
    const current = ++generation.current;
    setAllowed(false); setError(""); setDrivers([]);
    void request<Result>(`/api/v1/supervisor/drivers?company_id=${encodeURIComponent(companyID)}&search=${encodeURIComponent(search)}`, { signal: controller.signal })
      .then((result) => {
        if (controller.signal.aborted || generation.current !== current) return;
        setDrivers(result.drivers ?? []); setAllowed(true);
        setSelectedOps(Object.fromEntries((result.drivers ?? []).map((driver) => [driver.person_id, driver.operation_ids])));
      }).catch((err: unknown) => {
        if (controller.signal.aborted || generation.current !== current) return;
        setError(err instanceof SupervisorRequestError && err.status === 403
          ? "No tienes permiso para gestionar conductores de esta empresa."
          : err instanceof SupervisorRequestError && err.status === 401 ? "Tu sesión ha expirado. Vuelve a ingresar." : "No se pudo consultar la gestión de conductores.");
      });
    return () => controller.abort();
  }, [companyID, search, revision]);

  const mutate = async (method: "POST" | "PATCH", body: object) => {
    setBusy(true); setMessage(""); setError("");
    const payload = JSON.stringify({ companyID, method, body });
    if (retry.current?.payload !== payload) retry.current = { payload, key: crypto.randomUUID() };
    try {
      const result = await request<Result>(`/api/v1/supervisor/drivers?company_id=${encodeURIComponent(companyID)}`, {
        method, headers: { "Idempotency-Key": retry.current.key }, body: JSON.stringify(body),
      });
      retry.current = null;
      setMessage(result.status === "REVIEW_REQUIRED" ? "Solicitud de revisión recibida. La vinculación requiere autorización adicional de la plataforma." : method === "POST" ? "Conductor registrado. Ya puedes generar una invitación si tienes permiso de dispositivos." : "Relación empresarial actualizada.");
      setRevision((value) => value + 1); onChanged();
      if (method === "POST") { setShowForm(false); setForm({ given_name: "", family_name: "", identifier: "", phone: "", operation_ids: [] }); }
    } catch (err) {
      setError(err instanceof SupervisorRequestError && err.status === 403 ? "No tienes permiso para realizar esta acción."
        : err instanceof SupervisorRequestError && err.status === 409 ? "El conductor ya está registrado o cambió su versión. Actualiza el listado antes de reintentar."
          : "No se pudo guardar. Revisa los campos y vuelve a intentar.");
    } finally { setBusy(false); }
  };
  const toggle = (values: string[], id: string) => values.includes(id) ? values.filter((value) => value !== id) : [...values, id];
  return <section className="mt-6 rounded-2xl border border-teal-200 bg-teal-50 p-5" aria-label="Gestión de conductores">
    <div className="flex flex-wrap items-center justify-between gap-3"><h3 className="text-xl font-black">Gestión de conductores</h3>
      {allowed && <button className={buttonClass} disabled={busy} onClick={() => setShowForm(!showForm)}>Registrar conductor</button>}</div>
    {error && <p role="alert" className="mt-3 text-sm text-rose-800">{error}</p>}
    {message && <p role="status" className="mt-3 text-sm text-teal-900">{message}</p>}
    {allowed && <>
      <label className="mt-4 block text-sm font-semibold">Buscar conductor<input className={inputClass} value={search} onChange={(event) => setSearch(event.target.value)} maxLength={100} disabled={busy} /></label>
      <button className="mt-2 text-sm underline" disabled={busy} onClick={() => setRevision((value) => value + 1)}>Actualizar conductores</button>
      {showForm && <form className="mt-4 grid gap-3 rounded-xl bg-white p-4 sm:grid-cols-2" onSubmit={(event) => { event.preventDefault(); void mutate("POST", form); }}>
        <label>Nombre<input required maxLength={100} className={inputClass} value={form.given_name} onChange={(e) => setForm({ ...form, given_name: e.target.value })} /></label>
        <label>Apellidos<input required maxLength={100} className={inputClass} value={form.family_name} onChange={(e) => setForm({ ...form, family_name: e.target.value })} /></label>
        <label>Identificador de acceso<input required minLength={3} maxLength={320} className={inputClass} value={form.identifier} onChange={(e) => setForm({ ...form, identifier: e.target.value })} autoComplete="off" /></label>
        <label>Teléfono de contacto<input required type="tel" placeholder="+56912345678" pattern="\+[1-9][0-9]{6,14}" className={inputClass} value={form.phone} onChange={(e) => setForm({ ...form, phone: e.target.value })} /></label>
        <fieldset className="sm:col-span-2"><legend className="font-semibold">Operaciones autorizadas</legend>{operations.map((op) => <label className="mr-4 inline-flex min-h-11 items-center gap-2" key={op.id}><input type="checkbox" checked={form.operation_ids.includes(op.id)} onChange={() => setForm({ ...form, operation_ids: toggle(form.operation_ids, op.id) })} />{op.name}</label>)}</fieldset>
        <p className="text-xs text-slate-600 sm:col-span-2">La empresa es la seleccionada en la consola. No se requiere contraseña; el dispositivo se habilita mediante invitación y aprobación independiente.</p>
        <button disabled={busy} className={buttonClass} type="submit">Guardar conductor</button>
      </form>}
      <div className="mt-4 grid gap-3">{drivers.length === 0 && <p>No hay conductores en este alcance.</p>}{drivers.map((driver) => <article className="rounded-xl bg-white p-4" key={driver.person_id}>
        <p className="font-bold">{driver.legal_name}</p><p className="break-all text-sm">{driver.identifier}</p><p className="text-sm">{driver.phone || "Sin teléfono registrado"}</p>
        <p className="mt-2 text-sm">Relación empresarial: {driver.status === "ACTIVE" ? "Habilitada" : driver.status === "SUSPENDED" ? "Suspendida" : "Requiere revisión"}. Enrolamiento: {driver.enrollment_status === "ACTIVE" ? "Dispositivo autorizado" : driver.enrollment_status === "PENDING" ? "Activación en curso" : "Sin enrolamiento activo"}.</p>
        <fieldset disabled={busy}><legend className="mt-3 text-sm font-semibold">Operaciones de {driver.legal_name}</legend>{operations.map((op) => <label className="mr-4 inline-flex min-h-11 items-center gap-2" key={op.id}><input type="checkbox" checked={(selectedOps[driver.person_id] ?? driver.operation_ids).includes(op.id)} onChange={() => setSelectedOps({ ...selectedOps, [driver.person_id]: toggle(selectedOps[driver.person_id] ?? driver.operation_ids, op.id) })} />{op.name}</label>)}</fieldset>
        <div className="mt-3 flex flex-wrap gap-2">
          {["ACTIVE", "SUSPENDED"].includes(driver.status) && <><button className={buttonClass} disabled={busy} onClick={() => void mutate("PATCH", { person_id: driver.person_id, version: driver.version, status: driver.status, operation_ids: selectedOps[driver.person_id] ?? driver.operation_ids })}>Guardar operaciones</button>
            <button className={buttonClass} disabled={busy} onClick={() => { if (window.confirm(`¿Confirmas ${driver.status === "ACTIVE" ? "suspender" : "habilitar"} la relación de este conductor con tu empresa?`)) void mutate("PATCH", { person_id: driver.person_id, version: driver.version, status: driver.status === "ACTIVE" ? "SUSPENDED" : "ACTIVE", operation_ids: selectedOps[driver.person_id] ?? driver.operation_ids }); }}>{driver.status === "ACTIVE" ? "Suspender en empresa" : "Habilitar en empresa"}</button></>}
          {driver.status === "ACTIVE" && canInvite(driver.person_id) && <button className={buttonClass} disabled={busy} onClick={() => onInvite(driver.person_id)}>Preparar invitación por WhatsApp</button>}
        </div>
      </article>)}</div>
    </>}
  </section>;
}
