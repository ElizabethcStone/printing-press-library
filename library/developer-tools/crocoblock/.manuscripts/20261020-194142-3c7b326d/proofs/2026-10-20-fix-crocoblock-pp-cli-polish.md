# Polish de crocoblock-pp-cli — 2026-10-20

Recomendación: **ship** dentro del pipeline. No se publicó, promovió ni creó un PR. El lock del run se conservó. Se aplicó un único ciclo de correcciones y verificación; Go quedó congelado después de los tres archivos indicados abajo.

| Comprobación | Antes | Después |
|---|---:|---:|
| Scorecard con muestras reales de snapshots | 82/100 | 82/100 |
| Scorecard estructural | 83/100 | 83/100 |
| Verify | 73/73, 100% | 73/73, 100% |
| Workflow verify | PASS | PASS |
| Verify skill | 49 recetas, 0 hallazgos | 49 recetas, 0 hallazgos |
| Dogfood estructural, resultado crudo | WARN: 9 helpers generados | WARN: los mismos 9 helpers |
| Seis análisis previstos / encontrados | 6/6 | 6/6 |
| Go vet | 0 | 0 |
| Gosec crudo | 56 | 56 |
| Gosec accionable tras revisión del código específico | 0 | 0 |
| Tools audit pendiente / gates | 0 / 0 | 0 / 0 |
| PII pendiente / gates | 9 / 0 | 0 / 0 |
| MCP con entradas imposibles por archivos bloqueados | 6 | 0 |
| MCP normal, binario recompilado | 81 herramientas | 75 herramientas |
| Publish validate | skipped (mid-pipeline) | skipped (mid-pipeline) |

El valor 87 comunicado antes de esta invocación es histórico; no se reprodujo en este baseline. Las cifras actuales son 82 con live-check y 83 sin él. El scorer estático describe 44 contratos de transporte del manifiesto; eso no es el catálogo MCP efectivo. El catálogo efectivo se comprobó por initialize y tools/list en el binario final recompilado, no en el staged anterior.

Live-check está **exercised**: tres muestras reales aprobadas (schema diff, filters impact y relations audit), dos ejemplos genéricos no aplicables a estas capturas (formulario 42 y CCT rooms), y un prerequisite_unsynced para la política de reservas del ejemplo. Se conservaron los ejemplos genéricos y sus fallos; no se sustituyeron por IDs reales ni se fabricaron capturas para subir la puntuación. El revisor independiente evaluó sólo los tres PASS.

La matriz real del padre, repetida sobre este código después del ajuste MCP, pasó **252/252 comprobaciones obligatorias, cero fallos y 246 conditional skips/unverified explícitos**. La matriz anterior había pasado 248/248 con 250 omisiones condicionales. Las omisiones no cuentan como pruebas aprobadas. El marker actualizado es `proofs/phase5-acceptance.json`; el resultado post-polish está en `proofs/2026-10-20-dogfood-results-post-polish.json`. La evidencia de ciclo de vida positivo y limpieza de los seis análisis está en `proofs/crocoblock-local-analysis-lifecycle.md`.

Cambios aplicados:

- Conservados los 61 comandos de dominio en el CLI; el MCP omite schema_diff, cct_reconcile, reservations_overlaps, fields_update, fields_restore y el import del framework, porque sus archivos obligatorios no pueden pasarse mediante el contrato MCP. Permanecen 56 herramientas de dominio y 19 del framework en modo normal. El modo read-only reduce esa superficie.
- Declarado snapshot como destino de escritura para fields update. El guard de rutas del framework no se modificó y no hay bypass mediante args.
- Añadidas pruebas para diez IDs requeridos, seis herramientas ausentes en MCP y presentes en CLI, y conservación de los análisis que leen CROCOBLOCK_DB. Dos tests principales y sus dieciséis subcasos pasaron; build de CLI y MCP, vet y catálogo runtime pasaron.
- README y SKILL explican el alcance MCP, el uso del CLI para archivos, el store preconfigurado y la necesidad de incluir relations al capturar para schema diff por defecto. Instalación canónica y bloques de capacidades generadas se conservaron.
- Anonimizados nueve matches en pruebas archivadas y registro de investigación; se corrigieron también sus fuentes para impedir la reintroducción en el empaquetado. Las rutas de identidad SSH de esas pruebas también se sustituyeron por marcadores. El auditor estricto resolvió 9, agregó 0 y dejó el ledger sin pendientes; no hubo aceptaciones masivas.
- Registro durable del ajuste en `.printing-press-patches/mcp-local-file-workflow-exclusions.json`; retro local en `proofs/polish-mcp-file-input-retro.md`.

Revisión de herramientas: se inspeccionaron las descripciones, argumentos y anotaciones de las 81 herramientas antes del cambio, comparándolas con la implementación y ayuda. La comprobación mecánica por sí sola no detectaba los seis flujos de archivos imposibles. Tras corregirlos, los 75 tools restantes conservan argumentos utilizables, IDs requeridos y hints conservadores. Los flujos de escritura reciben payload inline; las lecturas y análisis del store usan la configuración del servidor. La prueba no ejecutó nuevas mutaciones del laboratorio.

Hallazgos omitidos deliberadamente:

- Los nueve helpers muertos pertenecen al framework generado; borrarlos sólo para subir la puntuación perjudicaría la durabilidad del print. El dogfood crudo sigue WARN, sin fallo bloqueante. En el bloque estructurado PASS expresa que su gate no bloquea; no oculta el WARN crudo.
- Gosec encontró 44 observaciones en código generado, registradas como retro candidates. Las doce observaciones de código específico son cuatro rutas explícitas de archivo y ocho cierres/limpiezas; se revisaron los límites de tamaño, permisos privados, exclusión de symlinks/inodos, creación exclusiva y conservación del error principal. No se agregaron supresiones para maquillar el scan. El detalle está en `proofs/polish-gosec-triage.json`.
- Output-review advierte nombres repetidos en dependencies de filters impact. La captura conserva referencias equivalentes de dos claves de metadatos; no duplica matching_events ni cambia la evaluación de recibos. Se conserva como advertencia de presentación sin alterar el contrato de observaciones en este pase.
- Los tres ejemplos genéricos sin los prerrequisitos del sitio no sustituyen la evidencia de ciclo real ni indican un defecto funcional. No se contabilizaron como PASS.

No quedaron problemas intentados sin resolver. Las limitaciones operativas del MCP están documentadas y cubiertas por pruebas; las funciones siguen disponibles mediante el CLI. Otro polish repetiría las mismas comprobaciones y no cambiaría las limitaciones deliberadas del framework o los datos requeridos por ejemplos genéricos. El padre conserva la promoción, el empaquetado, la actualización final de proofs embebidos, la eliminación de DB privadas y la activación local.

Divergencia: se validó un clon local con remoto canónico de la biblioteca pública y no se encontró una integración Crocoblock publicada. Go1.26.10, Printing Press 4.33.8 y espacio libre suficiente comprobados. No se operó sobre otro trabajo ni se liberó el lock activo.

---POLISH-RESULT---
scorecard_before: 82
scorecard_after: 82
verify_before: 100
verify_after: 100
dogfood_before: PASS
dogfood_after: PASS
dogfood_live_matrix_before: exercised
dogfood_live_matrix_after: exercised
govet_before: 0
govet_after: 0
gosec_before: 0
gosec_after: 0
tools_audit_before: 0
tools_audit_after: 0
publish_validate_before: skipped (mid-pipeline)
publish_validate_after: skipped (mid-pipeline)
fixes_applied:
- Excluidas seis herramientas MCP que no podían recibir archivos obligatorios, conservando sus comandos CLI.
- Anotado el destino snapshot y probadas exclusiones, presencia CLI y diez IDs requeridos.
- Documentados 61 comandos CLI y 75 tools MCP efectivos, además de relations para las capturas de schema diff.
- Resueltos nueve matches de privacidad en fuentes y copias embebidas; ledger PII estricto limpio.
- Registrados patch durable y retro local del mirror MCP.
skipped_findings:
- Dogfood crudo WARN por nueve helpers generados conservados; ningún gate bloqueante.
- Gosec 44 observaciones generadas derivadas a retro y doce observaciones específicas revisadas como no accionables.
- Advertencia de formato por referencias repetidas en dependencies, sin duplicación de eventos ni cambio de resultados.
- Dos ejemplos genéricos sin captura específica y un prerequisite de política ausente; evidencia de ciclo real separada.
remaining_issues: []
ship_recommendation: ship
further_polish_recommended: no
further_polish_reasoning: Los gates actuales pasan; las advertencias restantes corresponden al framework generado, referencias de metadatos conservadas o prerrequisitos de ejemplos genéricos ya cubiertos por pruebas de ciclo real.
---END-POLISH-RESULT---
