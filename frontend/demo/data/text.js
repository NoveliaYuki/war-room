/** Sample demo wording in each interface language. English matches the text saved by earlier demo seeds. */

/** Uses Catalan "d'" before a vowel or h, and "de " otherwise. */
const ofCa = (word) => (/^[aeiouàèéíïòóúüh]/i.test(word) ? `d'${word}` : `de ${word}`);

const euro = (locale) => (value) => new Intl.NumberFormat(locale, { style: "currency", currency: "EUR", maximumFractionDigits: 0 }).format(value);

const en = {
  questions: {
    HR: [
      "What kind of work gives you the most energy?",
      "How do you prefer to receive feedback?",
      "What would make your next role a strong long-term fit?",
    ],
    Technical: [
      "How would you break this problem into smaller parts?",
      "What trade-offs shaped your most recent system design?",
      "How do you investigate a production issue you cannot reproduce locally?",
      "Which engineering decision would you revisit with what you know now?",
    ],
    Cultural: [
      "Tell me about a time you changed your mind after hearing another perspective.",
      "How do you keep a distributed team aligned?",
      "What does a healthy disagreement look like to you?",
    ],
    "Offer & Decision": [
      "What would success look like in the first six months?",
      "How does the team support growth into broader responsibilities?",
      "What are the remaining steps and decision timeline?",
    ],
  },
  stageNotes: {
    HR: "The interviewer outlined the team's priorities and asked about the candidate's recent work.",
    Technical: "Discussed design trade-offs, failure handling, and how the approach would scale.",
    Cultural: "Compared collaboration styles and shared examples of giving and receiving feedback.",
    "Offer & Decision": "Reviewed role expectations, success measures, and the sample decision timeline.",
  },
  interviewerRoles: { HR: "Talent Partner", Technical: "Senior Engineer", Cultural: "Engineering Manager", "Offer & Decision": "Hiring Manager" },
  roundTitle: (stageType, round) => `${stageType} Round ${round}`,
  stageDescription: (stageType, role, company) => `${stageType} conversation for the ${role} role at ${company}.`,
  completedNote: (note) => `Demo conversation note: ${note}`,
  prepNote: (name) => `Demo prep note: Ask ${name} about the team's priorities, success measures, and next steps.`,
  interviewerNote: (stageType) => `Fictional ${stageType.toLowerCase()} interviewer for this sample process.`,
  answers: ["Shared a concise example with measurable impact.", "Explained the trade-offs and how the team aligned.", "Connected the decision to reliability and customer needs."],
  completedAnswer: (answer) => `Sample response: ${answer}`,
  prepTopics: ["measurable impact", "trade-offs and alignment", "reliability and customer needs"],
  prepAnswer: (topic) => `Prep: have a concise example ready about ${topic}.`,
  interviewPrep: [
    "Prepare concise examples with context, decisions, and measurable outcomes.",
    "Practice explaining technical trade-offs in plain language and connecting them to user or business impact.",
    "Bring thoughtful questions about the team, role expectations, and how success is measured.",
  ],
  interviewFollowUps: ["Ask how the team plans work across a typical quarter.", "Clarify the balance between hands-on delivery and technical leadership.", "Ask what the first 90 days would look like for this role."],
  agencies: ["Northstar Talent (sample)", "Bridgeway Search (sample)", "Juniper People (sample)"],
  talentTeam: (company) => `${company} Talent Acquisition`,
  description: (role) => `${role.title} on ${role.company}'s ${role.arrangement.replace("_", " ")} team. The role focuses on ${role.keywords.toLowerCase()}. Technologies mentioned in this fictional sample posting: ${role.technologies.join(", ")}. Responsibilities include designing and shipping reliable software, reviewing technical proposals, partnering with product and design, and improving service quality. The team works across engineering, product, and design; success is measured through customer outcomes, delivery quality, and operational health.\n\nFictional sample opportunity for the War Room demo; this is not an active job listing.`,
  companyOverview: (company) => `${company} is shown as a sample employer in this fictional interview pipeline. Company identity and logo are real; the role details, recruiter, interviewers, and hiring activity are illustrative demo data.`,
  reasonsToChange: (focus) => `Looking for broader ownership of ${focus}, with a team that values thoughtful engineering and clear customer impact. The move would build on current experience while providing room to grow in scope.`,
  experienceNotes: (focus, title, senior) => `Relevant sample experience: delivered production features related to ${focus}, partnered with product and design to scope work, and used monitoring and user feedback to improve reliability. For the ${title} level, examples emphasize ${senior ? "technical direction, mentoring, and cross-team outcomes" : "independent feature ownership, code quality, and effective collaboration"}.`,
  // Earlier seeds formatted these amounts with the browser's default locale, so English keeps doing so.
  salaryRange: (min, max) => `€${min.toLocaleString()}–€${max.toLocaleString()}`,
  salaryUpTo: (max) => `Up to €${max.toLocaleString()}`,
  salaryFrom: (min) => `From €${min.toLocaleString()}`,
  historicalDescription: (role, company) => `${role} at ${company}. Fictional sample opportunity from June 2021 for the War Room demo.`,
  historicalOverview: (company) => `${company} is a fictional sample employer in the June 2021 demo search period.`,
  historicalKeywords: (role) => `${role} • June 2021 sample role`,
  periods: { current: "Current", june2021: "June 2021" },
  importDraft: {
    overview: "## About the role\n\nBuild the platform behind a fictional demo company.\n\n## Requirements\n\n- Python, AWS and Terraform experience",
    title: "Senior Platform Engineer",
    company: "Demo Company",
    keywords: "Remote, Europe",
  },
};

const es = {
  roles: [
    { title: "Ingeniero/a frontend sénior", keywords: "Búsqueda accesible de alto tráfico • Arquitectura React • Rendimiento medible y Core Web Vitals", focus: "la interfaz de búsqueda accesible y de alto tráfico" },
    { title: "Ingeniero/a de producto sénior", keywords: "Flujos de producto con IA • React y Python full-stack • Evaluación, seguridad y fiabilidad del producto", focus: "los flujos de producto con IA" },
    { title: "Ingeniero/a de software II", keywords: "Plataforma GPU • Servicios en Go • Ingeniería de fiabilidad a escala global", focus: "la plataforma de computación GPU" },
    { title: "Ingeniero/a macOS sénior", keywords: "Funciones nativas de macOS • Swift y AppKit • Privacidad, accesibilidad y rendimiento", focus: "las funciones nativas de macOS" },
    { title: "Ingeniero/a de producto sénior", keywords: "Pagos para comercios • API en TypeScript • Idempotencia, casos límite y UX clara", focus: "los flujos de pago para comercios" },
    { title: "Ingeniero/a backend", keywords: "Escucha personalizada • Servicios en Go • Recomendaciones y datos de baja latencia", focus: "la escucha personalizada" },
    { title: "Ingeniero/a de software principal", keywords: "Arquitectura de Azure Storage • Sistemas distribuidos • Diseño entre equipos y fiabilidad del servicio", focus: "la arquitectura de Azure Storage" },
    { title: "Ingeniero/a backend sénior", keywords: "Ingesta de telemetría • Go y Kubernetes • Servicios de alto rendimiento y consultas eficientes", focus: "la ingesta de telemetría" },
    { title: "Responsable de ingeniería", keywords: "Hoja de ruta de infraestructura de producto • Desarrollo del equipo de ingeniería • Fiabilidad a escala", focus: "la hoja de ruta de infraestructura de producto" },
    { title: "Ingeniero/a de seguridad de plataforma", keywords: "Seguridad de plataformas en la nube • Políticas de Kubernetes • Infraestructura como código", focus: "la seguridad de plataformas en la nube" },
    { title: "Ingeniero/a de plataforma de IA", keywords: "Operación de la pasarela de LLM • Identidad y secretos • Observabilidad del uso", focus: "la operación de la pasarela de LLM" },
    { title: "Ingeniero/a de seguridad de aplicaciones", keywords: "Seguridad de aplicaciones • Ciclo de desarrollo seguro • Corrección de vulnerabilidades", focus: "la seguridad de aplicaciones" },
  ],
  historicalRoles: ["Diseñador/a UX", "Analista de datos", "Responsable de producto", "Ingeniero/a iOS", "Ingeniero/a de éxito del cliente", "Ingeniero/a de automatización de QA", "Diseñador/a de producto", "Ingeniero/a backend", "Ingeniero/a de datos", "Redactor/a técnico/a"],
  questions: {
    HR: [
      "¿Qué tipo de trabajo te da más energía?",
      "¿Cómo prefieres recibir comentarios sobre tu trabajo?",
      "¿Qué haría que tu próximo puesto encajara bien a largo plazo?",
    ],
    Technical: [
      "¿Cómo dividirías este problema en partes más pequeñas?",
      "¿Qué ventajas e inconvenientes pesaron en tu diseño de sistemas más reciente?",
      "¿Cómo investigas un problema de producción que no puedes reproducir en local?",
      "¿Qué decisión de ingeniería replantearías con lo que sabes ahora?",
    ],
    Cultural: [
      "Háblame de una vez en que cambiaste de opinión tras escuchar otro punto de vista.",
      "¿Cómo mantienes alineado a un equipo distribuido?",
      "¿Cómo es para ti un desacuerdo sano?",
    ],
    "Offer & Decision": [
      "¿Cómo sería el éxito en los primeros seis meses?",
      "¿Cómo apoya el equipo el crecimiento hacia responsabilidades más amplias?",
      "¿Cuáles son los pasos restantes y el calendario de decisión?",
    ],
  },
  stageNotes: {
    HR: "La persona entrevistadora explicó las prioridades del equipo y preguntó por el trabajo reciente de la persona candidata.",
    Technical: "Se habló de las ventajas e inconvenientes del diseño, la gestión de fallos y cómo escalaría la solución.",
    Cultural: "Se compararon estilos de colaboración y se compartieron ejemplos de cómo dar y recibir comentarios.",
    "Offer & Decision": "Se revisaron las expectativas del puesto, los indicadores de éxito y el calendario de decisión de ejemplo.",
  },
  interviewerRoles: { HR: "Responsable de selección", Technical: "Ingeniero/a sénior", Cultural: "Responsable de ingeniería", "Offer & Decision": "Responsable de contratación" },
  stageKinds: { HR: "de RR. HH.", Technical: "técnica", Cultural: "cultural", "Offer & Decision": "de oferta y decisión" },
  roundTitle: (stageType, round) => `Ronda técnica ${round}`,
  stageDescription(stageType, role, company) { return `Entrevista ${this.stageKinds[stageType]} para el puesto de ${role} en ${company}.`; },
  completedNote: (note) => `Resumen de la demo: ${note}`,
  prepNote: (name) => `Preparación de la demo: pregunta a ${name} por las prioridades del equipo, los indicadores de éxito y los próximos pasos.`,
  interviewerNote(stageType) { return `Persona entrevistadora ficticia para la fase ${this.stageKinds[stageType]} de este proceso de ejemplo.`; },
  answers: ["Compartí un ejemplo conciso con un impacto medible.", "Expliqué las ventajas e inconvenientes y cómo se alineó el equipo.", "Relacioné la decisión con la fiabilidad y las necesidades de los clientes."],
  completedAnswer: (answer) => `Respuesta de ejemplo: ${answer}`,
  prepTopics: ["el impacto medible", "las ventajas e inconvenientes y la alineación", "la fiabilidad y las necesidades de los clientes"],
  prepAnswer: (topic) => `Preparación: ten listo un ejemplo conciso sobre ${topic}.`,
  interviewPrep: [
    "Prepara ejemplos concisos con contexto, decisiones y resultados medibles.",
    "Practica cómo explicar las ventajas e inconvenientes técnicos en lenguaje sencillo y relacionarlos con el impacto en los usuarios o el negocio.",
    "Lleva preguntas meditadas sobre el equipo, las expectativas del puesto y cómo se mide el éxito.",
  ],
  interviewFollowUps: ["Pregunta cómo planifica el equipo el trabajo en un trimestre típico.", "Aclara el equilibrio entre el trabajo técnico directo y el liderazgo técnico.", "Pregunta cómo serían los primeros 90 días en este puesto."],
  agencies: ["Northstar Talent (ejemplo)", "Bridgeway Search (ejemplo)", "Juniper People (ejemplo)"],
  talentTeam: (company) => `Selección de talento de ${company}`,
  arrangements: { remote: "en remoto", hybrid: "híbrido", on_site: "presencial" },
  description(role) { return `${role.title} en ${role.company} (trabajo ${this.arrangements[role.arrangement]}). Áreas clave: ${role.keywords}. Tecnologías mencionadas en esta oferta ficticia de ejemplo: ${role.technologies.join(", ")}. Las responsabilidades incluyen diseñar y lanzar software fiable, revisar propuestas técnicas, colaborar con producto y diseño, y mejorar la calidad del servicio. El equipo trabaja entre ingeniería, producto y diseño; el éxito se mide por los resultados para los clientes, la calidad de las entregas y la salud operativa.\n\nOportunidad ficticia de ejemplo para la demo de War Room; no es una oferta de empleo activa.`; },
  companyOverview: (company) => `${company} aparece como empleador de ejemplo en este proceso de selección ficticio. La identidad y el logotipo de la empresa son reales; los detalles del puesto, el reclutador, los entrevistadores y la actividad de contratación son datos de demostración.`,
  reasonsToChange: (focus) => `Busco asumir más responsabilidad sobre ${focus}, en un equipo que valore la ingeniería cuidadosa y un impacto claro en los clientes. El cambio aprovecharía la experiencia actual y daría margen para crecer en alcance.`,
  experienceNotes: (focus, title, senior) => `Experiencia de ejemplo relevante: entrega de funcionalidades en producción relacionadas con ${focus}, colaboración con producto y diseño para definir el trabajo, y uso de la monitorización y de los comentarios de los usuarios para mejorar la fiabilidad. Para este nivel, los ejemplos destacan ${senior ? "la dirección técnica, la mentoría y los resultados entre equipos" : "la autonomía en las funcionalidades, la calidad del código y la colaboración eficaz"}.`,
  salaryRange: (min, max) => `${euro("es-ES")(min)}–${euro("es-ES")(max)}`,
  salaryUpTo: (max) => `Hasta ${euro("es-ES")(max)}`,
  salaryFrom: (min) => `Desde ${euro("es-ES")(min)}`,
  historicalDescription: (role, company) => `${role} en ${company}. Oportunidad ficticia de ejemplo de junio de 2021 para la demo de War Room.`,
  historicalOverview: (company) => `${company} es un empleador ficticio de ejemplo del periodo de búsqueda de junio de 2021 de la demo.`,
  historicalKeywords: (role) => `${role} • Puesto de ejemplo de junio de 2021`,
  periods: { current: "Actual", june2021: "Junio de 2021" },
  importDraft: {
    overview: "## Sobre el puesto\n\nConstruye la plataforma de una empresa ficticia de demostración.\n\n## Requisitos\n\n- Experiencia con Python, AWS y Terraform",
    title: "Ingeniero/a de plataforma sénior",
    company: "Empresa de demostración",
    keywords: "En remoto, Europa",
  },
};

const ca = {
  roles: [
    { title: "Enginyer/a frontend sènior", keywords: "Cerca accessible d'alt trànsit • Arquitectura React • Rendiment mesurable i Core Web Vitals", focus: "la interfície de cerca accessible i d'alt trànsit" },
    { title: "Enginyer/a de producte sènior", keywords: "Fluxos de producte amb IA • React i Python full-stack • Avaluació, seguretat i fiabilitat del producte", focus: "els fluxos de producte amb IA" },
    { title: "Enginyer/a de programari II", keywords: "Plataforma GPU • Serveis en Go • Enginyeria de fiabilitat a escala global", focus: "la plataforma de computació GPU" },
    { title: "Enginyer/a macOS sènior", keywords: "Funcions natives de macOS • Swift i AppKit • Privadesa, accessibilitat i rendiment", focus: "les funcions natives de macOS" },
    { title: "Enginyer/a de producte sènior", keywords: "Pagaments per a comerços • API en TypeScript • Idempotència, casos límit i UX clara", focus: "els fluxos de pagament per a comerços" },
    { title: "Enginyer/a backend", keywords: "Escolta personalitzada • Serveis en Go • Recomanacions i dades de baixa latència", focus: "l'escolta personalitzada" },
    { title: "Enginyer/a de programari principal", keywords: "Arquitectura d'Azure Storage • Sistemes distribuïts • Disseny entre equips i fiabilitat del servei", focus: "l'arquitectura d'Azure Storage" },
    { title: "Enginyer/a backend sènior", keywords: "Ingesta de telemetria • Go i Kubernetes • Serveis d'alt rendiment i consultes eficients", focus: "la ingesta de telemetria" },
    { title: "Responsable d'enginyeria", keywords: "Full de ruta d'infraestructura de producte • Desenvolupament de l'equip d'enginyeria • Fiabilitat a escala", focus: "el full de ruta d'infraestructura de producte" },
    { title: "Enginyer/a de seguretat de plataforma", keywords: "Seguretat de plataformes al núvol • Polítiques de Kubernetes • Infraestructura com a codi", focus: "la seguretat de plataformes al núvol" },
    { title: "Enginyer/a de plataforma d'IA", keywords: "Operació de la passarel·la de LLM • Identitat i secrets • Observabilitat de l'ús", focus: "l'operació de la passarel·la de LLM" },
    { title: "Enginyer/a de seguretat d'aplicacions", keywords: "Seguretat d'aplicacions • Cicle de desenvolupament segur • Correcció de vulnerabilitats", focus: "la seguretat d'aplicacions" },
  ],
  historicalRoles: ["Dissenyador/a UX", "Analista de dades", "Responsable de producte", "Enginyer/a iOS", "Enginyer/a d'èxit del client", "Enginyer/a d'automatització de QA", "Dissenyador/a de producte", "Enginyer/a backend", "Enginyer/a de dades", "Redactor/a tècnic/a"],
  questions: {
    HR: [
      "Quin tipus de feina et dona més energia?",
      "Com prefereixes rebre comentaris sobre la teva feina?",
      "Què faria que el teu proper lloc encaixés bé a llarg termini?",
    ],
    Technical: [
      "Com dividiries aquest problema en parts més petites?",
      "Quins avantatges i inconvenients van pesar en el teu disseny de sistemes més recent?",
      "Com investigues un problema de producció que no pots reproduir en local?",
      "Quina decisió d'enginyeria replantejaries amb el que saps ara?",
    ],
    Cultural: [
      "Explica'm una vegada que vas canviar d'opinió després d'escoltar un altre punt de vista.",
      "Com mantens alineat un equip distribuït?",
      "Com és per a tu un desacord sa?",
    ],
    "Offer & Decision": [
      "Com seria l'èxit en els primers sis mesos?",
      "Com dona suport l'equip al creixement cap a responsabilitats més àmplies?",
      "Quins són els passos que queden i el calendari de decisió?",
    ],
  },
  stageNotes: {
    HR: "La persona entrevistadora va explicar les prioritats de l'equip i va preguntar per la feina recent de la persona candidata.",
    Technical: "Es va parlar dels avantatges i inconvenients del disseny, la gestió d'errors i com escalaria la solució.",
    Cultural: "Es van comparar estils de col·laboració i es van compartir exemples de com donar i rebre comentaris.",
    "Offer & Decision": "Es van revisar les expectatives del lloc, els indicadors d'èxit i el calendari de decisió d'exemple.",
  },
  interviewerRoles: { HR: "Responsable de selecció", Technical: "Enginyer/a sènior", Cultural: "Responsable d'enginyeria", "Offer & Decision": "Responsable de contractació" },
  stageKinds: { HR: "de RH", Technical: "tècnica", Cultural: "cultural", "Offer & Decision": "d'oferta i decisió" },
  roundTitle: (stageType, round) => `Ronda tècnica ${round}`,
  stageDescription(stageType, role, company) { return `Entrevista ${this.stageKinds[stageType]} per al lloc ${ofCa(role)} a ${company}.`; },
  completedNote: (note) => `Resum de la demo: ${note}`,
  prepNote: (name) => `Preparació de la demo: pregunta a ${name} per les prioritats de l'equip, els indicadors d'èxit i els passos següents.`,
  interviewerNote(stageType) { return `Persona entrevistadora fictícia per a la fase ${this.stageKinds[stageType]} d'aquest procés d'exemple.`; },
  answers: ["Vaig compartir un exemple concís amb un impacte mesurable.", "Vaig explicar els avantatges i inconvenients i com es va alinear l'equip.", "Vaig relacionar la decisió amb la fiabilitat i les necessitats dels clients."],
  completedAnswer: (answer) => `Resposta d'exemple: ${answer}`,
  prepTopics: ["l'impacte mesurable", "els avantatges i inconvenients i l'alineació", "la fiabilitat i les necessitats dels clients"],
  prepAnswer: (topic) => `Preparació: tingues a punt un exemple concís sobre ${topic}.`,
  interviewPrep: [
    "Prepara exemples concisos amb context, decisions i resultats mesurables.",
    "Practica com explicar els avantatges i inconvenients tècnics en un llenguatge senzill i relacionar-los amb l'impacte en els usuaris o el negoci.",
    "Porta preguntes ben pensades sobre l'equip, les expectatives del lloc i com es mesura l'èxit.",
  ],
  interviewFollowUps: ["Pregunta com planifica l'equip la feina en un trimestre típic.", "Aclareix l'equilibri entre la feina tècnica directa i el lideratge tècnic.", "Pregunta com serien els primers 90 dies en aquest lloc."],
  agencies: ["Northstar Talent (exemple)", "Bridgeway Search (exemple)", "Juniper People (exemple)"],
  talentTeam: (company) => `Selecció de talent ${ofCa(company)}`,
  arrangements: { remote: "en remot", hybrid: "híbrida", on_site: "presencial" },
  description(role) { return `${role.title} a ${role.company} (feina ${this.arrangements[role.arrangement]}). Àrees clau: ${role.keywords}. Tecnologies esmentades en aquesta oferta fictícia d'exemple: ${role.technologies.join(", ")}. Les responsabilitats inclouen dissenyar i llançar programari fiable, revisar propostes tècniques, col·laborar amb producte i disseny, i millorar la qualitat del servei. L'equip treballa entre enginyeria, producte i disseny; l'èxit es mesura pels resultats per als clients, la qualitat dels lliuraments i la salut operativa.\n\nOportunitat fictícia d'exemple per a la demo de War Room; no és una oferta de feina activa.`; },
  companyOverview: (company) => `${company} apareix com a ocupador d'exemple en aquest procés de selecció fictici. La identitat i el logotip de l'empresa són reals; els detalls del lloc, el reclutador, els entrevistadors i l'activitat de contractació són dades de demostració.`,
  reasonsToChange: (focus) => `Busco assumir més responsabilitat sobre ${focus}, en un equip que valori l'enginyeria acurada i un impacte clar en els clients. El canvi aprofitaria l'experiència actual i donaria marge per créixer en abast.`,
  experienceNotes: (focus, title, senior) => `Experiència d'exemple rellevant: lliurament de funcionalitats en producció relacionades amb ${focus}, col·laboració amb producte i disseny per definir la feina, i ús de la monitorització i dels comentaris dels usuaris per millorar la fiabilitat. Per a aquest nivell, els exemples destaquen ${senior ? "la direcció tècnica, la mentoria i els resultats entre equips" : "l'autonomia en les funcionalitats, la qualitat del codi i la col·laboració eficaç"}.`,
  salaryRange: (min, max) => `${euro("ca-ES")(min)}–${euro("ca-ES")(max)}`,
  salaryUpTo: (max) => `Fins a ${euro("ca-ES")(max)}`,
  salaryFrom: (min) => `Des de ${euro("ca-ES")(min)}`,
  historicalDescription: (role, company) => `${role} a ${company}. Oportunitat fictícia d'exemple del juny del 2021 per a la demo de War Room.`,
  historicalOverview: (company) => `${company} és un ocupador fictici d'exemple del període de cerca del juny del 2021 de la demo.`,
  historicalKeywords: (role) => `${role} • Lloc d'exemple del juny del 2021`,
  periods: { current: "Actual", june2021: "Juny del 2021" },
  importDraft: {
    overview: "## Sobre el lloc\n\nConstrueix la plataforma d'una empresa fictícia de demostració.\n\n## Requisits\n\n- Experiència amb Python, AWS i Terraform",
    title: "Enginyer/a de plataforma sènior",
    company: "Empresa de demostració",
    keywords: "En remot, Europa",
  },
};

/** Sample wording keyed by interface language. */
export const demoText = { en, es, ca };

/** Returns the sample wording for a language, falling back to English. */
export function textFor(language) {
  return demoText[language] || en;
}
