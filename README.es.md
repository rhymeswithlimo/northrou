<div align="center">
<img alt="Northrou" src="public/repo/Hero_Banner_JPG_v1.0__Northrou.jpg" width="100%">
</div>

<h1 align="center">Northrou</h1>

<p align="center">
  <a href="README.md">English</a> ·
  <a href="README.zh-CN.md">简体中文</a> ·
  Español ·
  <a href="README.fr.md">Français</a> ·
  <a href="README.de.md">Deutsch</a> ·
  <a href="README.ja.md">日本語</a>
</p>

<p align="center">Tus películas y series, transmitidas desde tu propio hardware.</p>

<p align="center">
  <a href="https://northrou.sh">Sitio web</a> ·
  <a href="https://northrou.sh/docs">Documentación</a> ·
  <a href="#instalación">Instalación</a> ·
  <a href="#licencia">Licencia</a>
</p>

<a href="https://github.com/rhymeswithlimo/northrou/releases"><img src="https://img.shields.io/github/v/release/rhymeswithlimo/northrou" alt="Latest release"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-blue" alt="License: BSD 3-Clause"></a>
<a href="https://github.com/rhymeswithlimo/northrou/commits/main"><img src="https://img.shields.io/github/last-commit/rhymeswithlimo/northrou" alt="Last commit"></a>
<img src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.26+">

---

Northrou es un servidor multimedia de código abierto que ejecutas en tu propio hardware. Apúntalo a tu biblioteca de películas y series, y transmitirá contenido a tu teléfono, tablet, ordenador o TV — en casa o fuera — sin que tus archivos multimedia pasen nunca por servidores de terceros.

La reproducción se adapta a cada dispositivo. Los archivos se reproducen sin modificar allí donde el dispositivo pueda con ellos, convirtiendo solo lo que realmente lo necesita, aprovechando tu GPU cuando hay una disponible. Las pistas de Dolby Atmos y audio sin pérdida se transmiten tal cual o se adaptan según el dispositivo, en lugar de aplanarse a estéreo.

Añade una biblioteca y Northrou se encarga del resto: las carátulas, el reparto y los detalles se emparejan automáticamente, los subtítulos (incluidas las pistas basadas en imagen que la mayoría de servidores no pueden procesar) simplemente funcionan, y un motor de recomendaciones construido a partir de tu propio historial de visualización — que nunca se comparte con nadie — te ayuda a encontrar qué ver a continuación.

Una persona configura el servidor una sola vez y comparte un código de conexión. Todos los demás introducen ese código en la aplicación para conectarse — sin cuentas, sin correos, sin contraseñas. El acceso remoto es de igual a igual (peer-to-peer): tu servidor y tu dispositivo se comunican directamente, así que nada intermedio llega a ver lo que estás reproduciendo.

## Instalación

```sh
curl -sSL https://raw.githubusercontent.com/rhymeswithlimo/northrou/main/scripts/install.sh | sh
northrou setup
```

El instalador configura Northrou como un servicio en segundo plano y descarga FFmpeg automáticamente — no necesitas instalar nada más. Después, `setup` te guía paso a paso, directamente en la terminal y sin necesidad de navegador, para nombrar tu servidor, añadir tus carpetas multimedia y generar tu código de conexión. Instala la aplicación en tus otros dispositivos, introduce el código y listo, estarás conectado.

¿Prefieres Docker, o instalar todo manualmente? La guía completa (con todas las formas de instalación y opciones de configuración) está en [northrou.sh/docs](https://northrou.sh/docs), o consulta [docs/](docs/) en este repositorio.

## Comandos

En el día a día no deberías necesitar casi nada de esto — `northrou admin` abre un panel de terminal en vivo con las transmisiones, el hardware y la capacidad, por si alguna vez quieres mirar bajo el capó.

```text
northrou <command> [flags]

COMANDOS:
   setup                    configura el servidor desde la terminal (nombre, medios, clave TMDB, código)
   status                   muestra qué está haciendo el servidor y qué hacer a continuación
   doctor                   revisa la configuración e informa de cualquier problema
   serve                    ejecuta el servidor en primer plano (lo que invoca el servicio)
   install / uninstall      registra / elimina el servicio del sistema
   start / stop / restart   controla el servicio instalado
   logs                     muestra o sigue el registro reciente del servidor
   admin                    abre el panel de administración en vivo (TUI)
   scan [path...]           escanea una carpeta o unidad ahora (sin ruta escanea los directorios configurados)
   match <file>             fuerza un archivo a un título concreto de TMDB
   cc                       imprime el código de conexión de este servidor (para emparejar apps)
   cc rotate                sustituye el código de conexión y cierra la sesión en todos los dispositivos
   devices                  lista los dispositivos emparejados
   devices revoke <id>      cierra la sesión de un dispositivo emparejado
   tmdb-key                 muestra, establece o elimina la clave de la API de TMDB
   update                   busca e instala una versión más reciente
   version                  imprime información de la versión

GLOBAL (todos los comandos):
   --config string          ruta a config.toml (por defecto: el directorio de configuración del sistema)
   -v, --verbose             activa el registro de depuración
```

`setup` es interactivo y no necesita flags. Los comandos de servicio (`install`, `start`, `stop`, `restart`, `update`) escriben en ubicaciones propiedad de root, así que en Linux debes ejecutarlos con `sudo` — el propio comando te avisará cuando necesite privilegios elevados. Ejecuta `northrou <command> --help` para ver la lista completa de flags de cada comando.

## Documentación

La referencia completa — todas las opciones de configuración, la API HTTP, la arquitectura y más — está en [northrou.sh/docs](https://northrou.sh/docs). Las mismas páginas están reflejadas en este repositorio:

- [Referencia de configuración](docs/configuration.md)
- [Referencia de la API HTTP](docs/api.md)
- [Arquitectura](docs/architecture.md)
- [Cliente](docs/frontend.md)

## Desarrollo

Northrou es completamente de código abierto y se puede compilar por tu cuenta. Es un monorepo — el servidor y el broker de acceso remoto son módulos de Go independientes, y el cliente (`frontend/`) es una aplicación Tauri compartida entre web, escritorio, iOS y Android.

```sh
make build   # compila el cliente y luego bin/northrou y bin/coordinator
make test    # ejecuta la suite de pruebas
make run     # compila y ejecuta el servidor en local
```

Consulta [docs/architecture.md](docs/architecture.md) para ver cómo encajan las piezas.

## Licencia

BSD 3-Clause — ver [LICENSE](LICENSE). Puedes compilar, ejecutar, hacer fork y redistribuir el software libremente bajo esos términos. El nombre **Northrou**, los logotipos y los activos de marca no forman parte de esta cesión y no pueden usarse para respaldar o promocionar productos derivados sin permiso (ver [NOTICE](NOTICE)).
