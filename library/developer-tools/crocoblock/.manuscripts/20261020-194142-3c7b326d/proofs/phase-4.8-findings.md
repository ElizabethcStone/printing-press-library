# Phase 4.8 — Agentic SKILL Review

**Resultado final: PASS — F1–F4 resueltos, 0 errores y 0 warnings abiertos.**

Veredicto inicial: **FAIL — 4 errores corregibles; 0 warnings**. Revisión semántica del artefacto final posterior a shipcheck, con lectura de ayuda y fuente; sin mutaciones del laboratorio y sin editar documentación.

## Hallazgos

- **F1 — Stub/gated disclosure — error — SKILL.md:104.** `filters impact` sólo clasifica dependencias `custom_fields` de JetSmartFilters 3.8.6.2 con ambas claves válidas; los demás casos devuelven `unknown`. Fix: declarar este límite junto a la capacidad mediante su entrada en `research.json` y resincronizar.

- **F2 — Stub/gated disclosure — error — SKILL.md:113.** `reservations overlaps` exige ambos modelos completos, JetBooking 4.1.3.1 y JetAppointment 2.5.3; no expande recurrencias. Fix: explicitar versiones, capturas obligatorias y soporte slot/range junto a la capacidad en `research.json`, después resincronizar.

- **F3 — Stub/gated disclosure — error — SKILL.md:88.** `relations audit` no certifica ausencias WordPress: `status:any` excluye papelera y objetos restringidos. Sólo un inventario CCT completo sin filtros prueba ausencia. Fix: declarar que extremos WordPress ausentes quedan `unknown` junto a la capacidad en `research.json` y resincronizar.

- **F4 — Auth narrative accuracy — error — SKILL.md:320.** La narrativa exige archivo de identidad SSH, pero `--ssh-key` es opcional y el transporte acepta la configuración/agente SSH existente. Fix: marcar la identidad como opcional en `research.json.narrative.auth_narrative` y resincronizar README/SKILL.

## Los siete checks

| Check | Resultado | Evidencia |
| --- | --- | --- |
| 1. Trigger phrases match capabilities | PASS | Campos JetEngine, CCT, registros JFB, solapamientos y candidatos de filtros corresponden a comandos existentes; los triggers son peticiones naturales. |
| 2. Verified-set alignment | PASS | Unique Capabilities, research.novel_features_built y .printing-press.json contienen exactamente los mismos seis comandos, sin extras planificados. |
| 3. Novel-feature descriptions match commands | PASS en intención | Las seis descripciones corresponden al análisis implementado; los límites omitidos están registrados en F1–F3. |
| 4. Stub/gated disclosure | FAIL | F1–F3: contratos soportados más estrechos que lo visible al elegir esas capacidades. No se encontraron stubs presentados como implementados. |
| 5. Auth narrative accuracy | FAIL | F4. Las tres variables WordPress y la separación de sesión Crocoblock son correctas. `doctor`, `discover` y `auth` existen; no se promete un login ausente. |
| 6. Recipe output claims | PASS | Diff estructural, `forms fields --select fields`, conciliación sin valores y candidatos basados en journal coinciden con la ayuda. `--since-event` es exclusivo y está explicado en la tabla. |
| 7. Marketing-copy smell | PASS | Lenguaje operativo concreto; no se encontraron promesas promocionales de capacidades inexistentes. |

## Evidencia de los hallazgos

- F1: `internal/cli/filters_impact.go:81` exige versión `3.8.6.2`, `_data_source == custom_fields` y dos dependencias válidas; líneas 82–84 emiten `unknown` fuera de ese contrato.
- F2: `reservations overlaps --help` exige ambas capturas completas y limita las citas a slot/range; `internal/cli/reservations_overlaps.go:377` y `:380` fijan las versiones admitidas. El rechazo de modelos no expandidos está en `:260`.
- F3: `relations audit --help` explica que el inventario WordPress excluye trash; `internal/cli/relations_audit.go:206` conserva `unknown` para objetos ausentes. La condición de ausencia CCT aparece en `:307`.
- F4: `filters reindex --help` no marca `--ssh-key` requerido; `internal/cli/croco_reindex.go:22` agrega `IdentitiesOnly` y `-i` sólo cuando se proporciona una llave.

## Fuentes consultadas

- SKILL.md y README.md finales, absorb manifest aprobado y `.printing-press.json`.
- `research.json`: `novel_features`, `novel_features_built`, narrativa de auth, recetas y triggers.
- Ayuda actual de los seis análisis y comandos base relacionados, auth, doctor y discover; salida cruda en `phase-4.8-runtime-help.json`.
- Lectura acotada de los adaptadores para confirmar límites explícitos; no se ejecutaron operaciones live.

Las correcciones de contenido deben originarse en research.json y propagarse a los artefactos derivados. Este reporte no modifica esos archivos ni reemplaza la comprobación de comportamiento de fase 18.

## Resolución y segunda revisión

**PASS — no findings.** Los cuatro hallazgos se corrigieron primero en `research.json`: descripciones de `filters impact`, `reservations overlaps` y `relations audit`, más `narrative.auth_narrative`. Structural dogfood sincronizó documentos, whichIndex, Highlights y metadatos sin ejecutar live dogfood. Las tablas y guías locales permanecieron idénticas.

| Check repetido | Resultado |
| --- | --- |
| 1. Triggers corresponden a capacidades existentes | PASS |
| 2. Unique Capabilities coincide exactamente con seis funciones verificadas | PASS |
| 3. Descripciones corresponden a comportamiento y límites implementados | PASS |
| 4. Proveedores/versiones/modelos soportados y ausencias WordPress están declarados junto a la capacidad | PASS |
| 5. Auth WordPress correcta y llave SSH explícitamente opcional | PASS |
| 6. Recetas y formas de salida mantienen el contrato verificado | PASS |
| 7. Texto operativo concreto, sin promesas promocionales | PASS |

Evidencia de propagación: `phase-4.8-resolution.json` y ayuda del binario recompilado en `phase-4.8-runtime-help-after.json`. CLI y MCP compilaron con Go 1.26.10. La sincronización estructural devolvió WARN por helpers muertos ya existentes, con seis de seis funciones detectadas y sin nuevos problemas de contenido.

### Workarounds acotados de sincronización

El sincronizador no reconoce `registeredCommandMirrorCapabilities(s, []map[string]string{...})`: dejó las tres descripciones antiguas en el MCP incluso con `--overwrite-command-mirror`. Se copiaron únicamente esas tres cadenas desde research.json; una comparación byte por byte verificó que el wrapper, las claves `cli_command` y la lógica quedaron intactos.

El sincronizador también actualiza `novel_features` de `.printing-press.json` pero deja intacto un bloque `novel_features_built` previamente existente. Se propagaron las mismas tres descripciones al segundo bloque. Los Highlights conservan el truncamiento normal del generador; la descripción completa está en SKILL, README, which, MCP y ambos bloques del manifest.

No se modificó el comportamiento del CLI, la lógica del MCP ni el laboratorio. Los hallazgos iniciales se mantienen arriba como historial; el resultado vigente es PASS.

### Verificación mecánica final

`verify-skill` final: **PASS**, seis categorías, 49 recetas, cero hallazgos. La primera corrida atribuyó erróneamente `--ssh-key` del párrafo de auth a `discover`; se cambió el texto fuente a “The SSH identity file is optional…” y se resincronizó. La tabla de `filters reindex` conserva el flag real. Evidencia: `phase-4.8-verify-skill-final.json`.
