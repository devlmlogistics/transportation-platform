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
              <p className="mt-5 max-w-2xl text-base leading-7 text-slate-600">
                Monolito modular Go + React preparado para evolucionar hacia una plataforma multiempresa.
              </p>
            </div>
            <div className="rounded-2xl border border-amber-200 bg-amber-50 px-5 py-4 text-sm font-semibold leading-6 text-amber-900">
              Esta pantalla es una base técnica. No hay autenticación, datos operativos ni sesiones reales.
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
      </div>
    </main>
  );
}
