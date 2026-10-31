# Fuentes, repositorios y versiones

Fecha de inspección: 2026-10-31. Alcance: lectura de archivos, metadatos Git, catálogo PostgreSQL y estado Docker. No se modificaron los repositorios originales, sus ramas, sus dependencias ni la base de datos.

Convención:

- [V] hecho verificado directamente.
- [I] inferencia basada en código o configuración.
- [P] pendiente de confirmar.

## Acceso

| Fuente | Resultado | Evidencia |
|---|---|---|
| Drayage | [V] Accesible | C:/Users/hecto/OneDrive/Documentos/ChatGPT/Consulta drayage |
| LM Transportation / LM Agenda | [V] Accesible en el checkout raíz y sus checkouts anidados | C:/Users/hecto/OneDrive/Documentos/ChatGPT/Api Consulta lm |
| Dashboard-Go | [V] Accesible | C:/Users/hecto/OneDrive/Documentos/ChatGPT/Dashboard-Go |
| PostgreSQL de testing | [V] Accesible en pgdb16.8; imagen postgres:16.8-alpine3.21; base lmlogistics | Inspección Docker y catálogo PostgreSQL |

## Drayage

### Checkout de desarrollo

- [V] Repositorio Git en C:/Users/hecto/OneDrive/Documentos/ChatGPT/Consulta drayage.
- [V] Rama activa: codex/internal-forms.
- [V] HEAD: 5b82d758cd661ae0c51d9f669445486b0b60c54c; commit “Attach application to lm_red network”.
- [V] La rama local está adelantada respecto de main/origin/main y contiene cambios sin commit y archivos no rastreados. Entre ellos están la PWA, autenticación de dispositivos, migraciones 007/008 y cambios del portal supervisor.
- [V] La rama main/origin/main apunta a 99b09d5, “Open driver forms in same tab”.
- [I] Este checkout representa la línea de desarrollo de la aplicación monolítica Docker, con backend Node y frontend React.

Fuentes: metadata Git del checkout; package.json; compose.yaml; README-DOCKER.md; server/index.mjs; server/supervisor.mjs; db/migrations.

### Checkout de producción disponible

- [V] Existe C:/Users/hecto/OneDrive/Documentos/ChatGPT/Consulta drayage/tmp/sites-production.
- [V] Es un repositorio Git separado/anidado, rama main, HEAD 254de9d, “Update Site source”.
- [V] El checkout contiene el frontend y configuración de publicación de Sites/nginx, pero no contiene el servidor Node de la raíz de desarrollo.
- [I] Debe tratarse como snapshot de publicación del sitio, no como sustituto del backend operativo.
- [P] No se verificó el endpoint remoto ni el servidor de producción; por tanto, no se afirma que este checkout sea el artefacto actualmente servido por producción.

Fuentes: metadata Git de tmp/sites-production; lista de archivos de tmp/sites-production; README-DOCKER.md; app/api/internal/[...path]/route.ts; app/api/supervisor/[...path]/route.ts.

## LM Transportation / LM Agenda

### Repositorio contenedor

- [V] C:/Users/hecto/OneDrive/Documentos/ChatGPT/Api Consulta lm tiene un .git en la raíz, rama master sin commits.
- [V] El estado raíz muestra como no rastreados consulta_semanal, dashboard-transfer, oracle y tmp, entre otros.
- [P] El repositorio raíz no permite establecer una versión única del ecosistema; las versiones deben identificarse por cada repositorio anidado.

### Repositorio consulta_semanal

- [V] C:/Users/hecto/OneDrive/Documentos/ChatGPT/Api Consulta lm/consulta_semanal tiene Git propio.
- [V] Rama main, HEAD f5c7f0f, “formulario aceptacion drayage_”.
- [V] Está adelantada seis commits respecto de origin/main y tiene modificaciones locales y contenido no rastreado que incluye deploy, frontend, SQL y módulos de Agenda.
- [I] Es la fuente principal del backend Flask legacy y del frontend/servicios que fueron evolucionando hacia LM Agenda.

### Checkouts de despliegue

- [V] deploy/lm-agenda-backend: rama codex/dashboard-monitor, HEAD f3b74ba, siguiendo origin/codex/dashboard-monitor; tiene modificación local en src/viajes.py.
- [V] deploy/lm-agenda-front: rama codex/dashboard-monitor, HEAD e3e0b00, siguiendo origin/codex/dashboard-monitor; tiene modificación local en src/App.jsx.
- [V] tmp/sites-checkout-lm-driver-agenda: rama main, HEAD 393961b, adelantada un commit respecto de origin/main.
- [V] tmp/sites-checkout-lm-driver-modal: rama main, HEAD a0d7eee, alineada con origin/main.
- [V] tmp/sites-remote-sync: rama main, HEAD 38c6f97; no se observó remoto configurado en la salida de ramas.
- [I] Los checkouts de Sites son snapshots de frontend y no deben confundirse con el backend Flask.

Fuentes: metadata Git de cada ruta; consulta_semanal/app.py; consulta_semanal/src; deploy/lm-agenda-backend; deploy/lm-agenda-front; dashboard-transfer.

## Dashboard-Go

- [V] Repositorio Git en C:/Users/hecto/OneDrive/Documentos/ChatGPT/Dashboard-Go.
- [V] Rama master alineada con origin/master.
- [V] HEAD 7b1dd35, “Sort dashboard rows by type then driver”.
- [V] El checkout no mostró cambios locales.
- [V] El repositorio contiene un servidor Go, frontend React/Vite/Tailwind, Dockerfile multi-stage y compose local/cloud.
- [V] El contenedor local dashboard-go-dashboard-1 estaba detenido; el estado productivo indicado por el usuario no se validó contra un servidor remoto.

Fuentes: metadata Git; go.mod; ARCHITECTURE.md; README.md; compose.yaml; compose.cloud.yaml; docker ps -a.

## Estado Docker relacionado

- [V] pgdb16.8 está en ejecución, con postgres:16.8-alpine3.21 y el puerto del host 5432 publicado en IPv4/IPv6.
- [V] dashboard-go-dashboard-1 existe, pero estaba detenido.
- [V] No se reiniciaron, crearon, eliminaron ni modificaron contenedores.

## Limitaciones y protección de información

- [V] No se copiaron secretos, contraseñas, claves privadas, filas completas ni dumps a esta documentación.
- [V] La carpeta oracle contiene material sensible; no se leyó su contenido para el inventario.
- [V] Se excluyeron de la revisión de código los artefactos generados, node_modules, entornos virtuales, backups de conversación y dumps SQL cuando no eran necesarios para establecer arquitectura.
- [P] Debe confirmarse el commit exacto desplegado en cada servicio productivo y la configuración efectiva fuera de estos checkouts.
