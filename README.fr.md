<div align="center">
<img alt="Northrou" src="public/repo/Hero_Banner_JPG_v1.2__Northrou.jpg" width="100%">
</div>

<h3 align="center">Northrou</h3>

<p align="center">
  <a href="README.md">English</a> ·
  <a href="README.zh-CN.md">简体中文</a> ·
  <a href="README.es.md">Español</a> ·
  Français ·
  <a href="README.de.md">Deutsch</a> ·
  <a href="README.ja.md">日本語</a>
</p>

<p align="center">Vos films et séries, diffusés depuis votre propre matériel.</p>

<p align="center">
  <a href="https://northrou.sh">Site web</a> ·
  <a href="https://northrou.sh/docs">Documentation</a> ·
  <a href="#installation">Installation</a> ·
  <a href="#licence">Licence</a>
</p>

<p align="center">
<a href="https://github.com/rhymeswithlimo/northrou/releases"><img src="https://img.shields.io/github/v/release/rhymeswithlimo/northrou" alt="Latest release"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-blue" alt="License: BSD 3-Clause"></a>
<a href="https://github.com/rhymeswithlimo/northrou/commits/main"><img src="https://img.shields.io/github/last-commit/rhymeswithlimo/northrou" alt="Last commit"></a>
<img src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.26+">
</p>

---

Northrou est un serveur multimédia open source que vous exécutez sur votre propre matériel. Pointez-le vers votre bibliothèque de films et de séries, et il diffuse le contenu sur votre téléphone, votre tablette, votre ordinateur ou votre TV, chez vous ou à distance, sans que vos médias ne passent jamais par les serveurs de qui que ce soit d'autre.

La lecture s'adapte à ce qui la reçoit. Les fichiers sont lus sans modification partout où l'appareil peut les gérer, et seul ce qui en a réellement besoin est converti, en utilisant votre GPU quand il y en a un de disponible. Les pistes Dolby Atmos et audio sans perte sont transmises telles quelles ou adaptées selon l'appareil, plutôt que d'être aplaties en stéréo.

Ajoutez une bibliothèque et Northrou fait le reste : affiches, distribution et détails sont associés automatiquement, les sous-titres (y compris les pistes basées sur des images, que la plupart des serveurs ne savent pas traiter) fonctionnent sans effort, et un moteur de recommandation construit à partir de votre propre historique de visionnage, jamais partagé avec qui que ce soit, vous aide à trouver quoi regarder ensuite.

Une seule personne configure le serveur, une fois, puis partage un code de connexion. Tous les autres saisissent ce code dans l'application pour se connecter : sans compte, sans e-mail, sans mot de passe. L'accès à distance est pair-à-pair : votre serveur et votre appareil communiquent directement, si bien que rien entre les deux ne voit jamais ce que vous diffusez.

## Installation

```sh
curl -sSL https://raw.githubusercontent.com/rhymeswithlimo/northrou/main/scripts/install.sh | sh
northrou setup
```

Le programme d'installation configure Northrou comme service en arrière-plan et récupère FFmpeg automatiquement, sans rien d'autre à installer. `setup` vous guide ensuite, directement dans le terminal et sans navigateur, pour nommer votre serveur, ajouter vos dossiers multimédias et générer votre code de connexion. Installez l'application sur vos autres appareils, saisissez le code, et vous êtes connecté.

Vous préférez Docker, ou une installation manuelle ? Le guide complet (avec toutes les méthodes d'installation et options de configuration) se trouve sur [northrou.sh/docs](https://northrou.sh/docs), ou consultez [docs/](docs/) dans ce dépôt.

## Commandes

Au quotidien, vous ne devriez avoir besoin de presque rien de tout ça : `northrou admin` ouvre un tableau de bord en direct dans le terminal, avec les flux, le matériel et la capacité, si jamais vous voulez regarder sous le capot.

```text
northrou <command> [flags]

COMMANDES :
   setup                    configure le serveur dans votre terminal (nom, médias, clé TMDB, code)
   status                   affiche ce que fait le serveur et la prochaine étape
   doctor                   vérifie la configuration et signale les problèmes
   serve                    exécute le serveur au premier plan (ce qu'invoque le service)
   install / uninstall      enregistre / supprime le service système
   start / stop / restart   contrôle le service installé
   logs                     affiche ou suit les journaux récents du serveur
   admin                    ouvre le tableau de bord d'administration en direct (TUI)
   scan [path...]           scanne un dossier ou un disque immédiatement (sans chemin, scanne les dossiers configurés)
   match <file>             force un fichier vers un titre TMDB précis
   cc                       affiche le code de connexion de ce serveur (pour associer des applis)
   cc rotate                remplace le code de connexion et déconnecte tous les appareils
   devices                  liste les appareils associés
   devices revoke <id>      déconnecte un appareil associé
   tmdb-key                 affiche, définit ou supprime la clé d'API TMDB
   update                   recherche et installe une version plus récente
   version                  affiche les informations de version
   -h, --help               affiche l'aide d'une commande

GLOBAL (toutes les commandes) :
   --config string          chemin vers config.toml (par défaut : dossier de configuration du système)
   -v, --verbose            active la journalisation de débogage

LOGS :
   -f, --follow             continue d'afficher les nouvelles lignes de log au fur et à mesure
   -n, --lines int          nombre de dernières lignes à afficher (par défaut 200)

ADMIN :
   --addr string            URL de base du serveur (par défaut celle de la configuration, ex. http://localhost:8674)

SCAN :
   --tv                     traite les chemins donnés comme des épisodes de série (par défaut : détection via le nom de fichier)

MATCH :
   --tmdb-id int            ID TMDB du film ou de la série à lier (obligatoire)
   --tv                     traite le fichier comme un épisode de série
   --season int             numéro de saison (avec --tv)
   --episode int            numéro d'épisode (avec --tv)

CC ROTATE :
   -y, --yes                effectue la rotation sans confirmation

UPDATE :
   -y, --yes                applique la mise à jour sans confirmation
   --check                  vérifie seulement ; n'installe rien
```

`setup` est interactif et ne nécessite aucun flag. Les commandes de service (`install`, `start`, `stop`, `restart`, `update`) écrivent dans des emplacements appartenant à root, donc exécutez-les avec `sudo` sur Linux. La commande vous indique quand elle a besoin de privilèges élevés.

## Documentation

La référence complète, avec toutes les options de configuration, l'API HTTP, l'architecture, et plus encore, se trouve sur [northrou.sh/docs](https://northrou.sh/docs). Les mêmes pages sont reprises dans ce dépôt :

- [Référence de configuration](docs/configuration.md)
- [Référence de l'API HTTP](docs/api.md)
- [Architecture](docs/architecture.md)
- [Client](docs/frontend.md)

## Développement

Northrou est entièrement open source et peut être compilé soi-même. C'est un monorepo : le serveur et le courtier d'accès distant sont des modules Go distincts, et le client (`frontend/`) est une application Tauri partagée entre le web, le bureau, iOS et Android.

```sh
make build   # compile le client, puis bin/northrou et bin/coordinator
make test    # exécute la suite de tests
make run     # compile et exécute le serveur en local
```

Voir [docs/architecture.md](docs/architecture.md) pour comprendre comment les différentes pièces s'assemblent.

## Licence

BSD 3-Clause, voir [LICENSE](LICENSE). Vous pouvez compiler, exécuter, forker et redistribuer librement ce logiciel selon ces conditions. Le nom **Northrou**, les logos et les éléments de marque ne font pas partie de cette autorisation et ne peuvent pas être utilisés pour cautionner ou promouvoir des produits dérivés sans permission (voir [NOTICE](NOTICE)).
