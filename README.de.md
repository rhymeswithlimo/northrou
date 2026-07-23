<div align="center">
<img alt="Northrou" src="public/repo/Hero_Banner_JPG_v1.1__Northrou.jpg" width="100%">
</div>

<h3 align="center">Northrou</h3>

<p align="center">
  <a href="README.md">English</a> ·
  <a href="README.zh-CN.md">简体中文</a> ·
  <a href="README.es.md">Español</a> ·
  <a href="README.fr.md">Français</a> ·
  Deutsch ·
  <a href="README.ja.md">日本語</a>
</p>

<p align="center">Deine Filme und Serien, gestreamt von deiner eigenen Hardware.</p>

<p align="center">
  <a href="https://northrou.sh">Website</a> ·
  <a href="https://northrou.sh/docs">Dokumentation</a> ·
  <a href="#installation">Installation</a> ·
  <a href="#lizenz">Lizenz</a>
</p>

<p align="center">
<a href="https://github.com/rhymeswithlimo/northrou/releases"><img src="https://img.shields.io/github/v/release/rhymeswithlimo/northrou" alt="Latest release"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-blue" alt="License: BSD 3-Clause"></a>
<a href="https://github.com/rhymeswithlimo/northrou/commits/main"><img src="https://img.shields.io/github/last-commit/rhymeswithlimo/northrou" alt="Last commit"></a>
<img src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.26+">
</p>

---

Northrou ist ein Open-Source-Medienserver, den du auf deiner eigenen Hardware betreibst. Richte ihn auf deine Film- und Serienbibliothek aus, und er streamt sie auf dein Smartphone, Tablet, deinen Desktop oder Fernseher — zuhause oder unterwegs — ohne dass deine Medien jemals über die Server Dritter laufen.

Die Wiedergabe passt sich an, was gerade zuschaut. Dateien werden unverändert abgespielt, wo immer ein Gerät sie verarbeiten kann; umgewandelt wird nur, was tatsächlich nötig ist, und dabei wird deine GPU genutzt, sofern vorhanden. Dolby-Atmos- und verlustfreie Audiospuren werden durchgereicht oder je nach Gerät angepasst, statt auf Stereo reduziert zu werden.

Füge eine Bibliothek hinzu, und Northrou erledigt den Rest: Poster, Besetzung und Details werden automatisch zugeordnet, Untertitel (einschließlich bildbasierter Spuren, mit denen die meisten Server nichts anfangen können) funktionieren einfach, und eine Empfehlungs-Engine, die auf deinem eigenen Sehverlauf basiert — der nirgendwo geteilt wird — hilft dir, den nächsten Titel zu finden.

Eine Person richtet den Server einmal ein und teilt einen Verbindungscode. Alle anderen geben diesen Code in der App ein, um sich zu verbinden — keine Konten, keine E-Mails, keine Passwörter. Der Fernzugriff erfolgt Peer-to-Peer: Server und Gerät kommunizieren direkt miteinander, sodass nichts dazwischen jemals sieht, was du gerade streamst.

## Installation

```sh
curl -sSL https://raw.githubusercontent.com/rhymeswithlimo/northrou/main/scripts/install.sh | sh
northrou setup
```

Das Installationsskript richtet Northrou als Hintergrunddienst ein und lädt FFmpeg automatisch herunter — sonst musst du nichts installieren. `setup` führt dich anschließend direkt im Terminal, ganz ohne Browser, durch die Benennung deines Servers, das Hinzufügen deiner Medienordner und die Erzeugung deines Verbindungscodes. Installiere die App auf deinen anderen Geräten, gib den Code ein, und du bist verbunden.

Lieber Docker oder eine manuelle Installation? Die vollständige Anleitung (mit allen Installationswegen und Konfigurationsoptionen) findest du unter [northrou.sh/docs](https://northrou.sh/docs), oder sieh dir [docs/](docs/) in diesem Repository an.

## Befehle

Im Alltag brauchst du das meiste davon nicht — `northrou admin` öffnet ein Live-Dashboard im Terminal mit Streams, Hardware und Kapazität, falls du mal einen Blick unter die Haube werfen willst.

```text
northrou <command> [flags]

BEFEHLE:
   setup                    Server im Terminal einrichten (Name, Medien, TMDB-Schlüssel, Code)
   status                   zeigt, was der Server gerade tut und was als Nächstes zu tun ist
   doctor                   prüft die Einrichtung und meldet etwaige Probleme
   serve                    Server im Vordergrund ausführen (das ruft der Dienst auf)
   install / uninstall      Systemdienst registrieren / entfernen
   start / stop / restart   installierten Dienst steuern
   logs                     zeigt die letzten Server-Logs an oder folgt ihnen live
   admin                    öffnet das Live-Admin-Dashboard (TUI)
   scan [path...]           scannt sofort einen Ordner oder ein Laufwerk (ohne Pfad werden die konfigurierten Verzeichnisse gescannt)
   match <file>             ordnet eine Datei fest einem bestimmten TMDB-Titel zu
   cc                       gibt den Verbindungscode dieses Servers aus (zum Koppeln von Apps)
   cc rotate                ersetzt den Verbindungscode und meldet alle Geräte ab
   devices                  listet gekoppelte Geräte auf
   devices revoke <id>      meldet ein gekoppeltes Gerät ab
   tmdb-key                 zeigt, setzt oder entfernt den TMDB-API-Schlüssel
   update                   prüft auf eine neuere Version und installiert sie
   version                  gibt Versionsinformationen aus

GLOBAL (bei jedem Befehl):
   --config string          Pfad zur config.toml (Standard: OS-Konfigurationsverzeichnis)
   -v, --verbose             aktiviert Debug-Logging
```

`setup` ist interaktiv und braucht keine Flags. Dienst-Befehle (`install`, `start`, `stop`, `restart`, `update`) schreiben an Speicherorte, die root gehören, deshalb unter Linux mit `sudo` ausführen — der Befehl sagt dir, wenn er erhöhte Rechte braucht. Führe `northrou <command> --help` aus, um die vollständige Liste der Flags pro Befehl zu sehen.

## Dokumentation

Die vollständige Referenz — jede Konfigurationsoption, die HTTP-API, die Architektur und mehr — findest du unter [northrou.sh/docs](https://northrou.sh/docs). Dieselben Seiten sind auch in diesem Repository gespiegelt:

- [Konfigurationsreferenz](docs/configuration.md)
- [HTTP-API-Referenz](docs/api.md)
- [Architektur](docs/architecture.md)
- [Client](docs/frontend.md)

## Entwicklung

Northrou ist vollständig quelloffen und lässt sich selbst bauen. Es ist ein Monorepo — Server und Remote-Access-Broker sind getrennte Go-Module, und der Client (`frontend/`) ist eine Tauri-App, die für Web, Desktop, iOS und Android gemeinsam genutzt wird.

```sh
make build   # baut den Client, danach bin/northrou und bin/coordinator
make test    # führt die Testsuite aus
make run     # baut den Server und startet ihn lokal
```

Wie die Teile zusammenspielen, steht in [docs/architecture.md](docs/architecture.md).

## Lizenz

BSD 3-Clause — siehe [LICENSE](LICENSE). Du darfst die Software unter diesen Bedingungen frei bauen, ausführen, forken und weiterverbreiten. Der Name **Northrou**, Logos und Markenbestandteile sind nicht Teil dieser Lizenz und dürfen ohne Erlaubnis nicht verwendet werden, um davon abgeleitete Produkte zu unterstützen oder zu bewerben (siehe [NOTICE](NOTICE)).
