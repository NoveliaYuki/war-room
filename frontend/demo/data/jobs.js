/** Browser-only sample records for the interactive demo. */
import { formatLocalDate, getDemoMeetingDayOffset } from "./meetingDates.js";
import { technologyRecord } from "./technologies.js";
import { textFor } from "./text.js";

const roles = [
  { company: "Google", title: "Senior Frontend Engineer", status: "ongoing", currentStageIndex: 1, stages: ["HR", "Technical", "Cultural", "Offer & Decision"], keywords: "Accessible high-traffic search UI • React architecture • Measurable performance and Core Web Vitals", technologies: ["React", "TypeScript", "JavaScript", "Kubernetes", "Go"], domain: "google.com", salary: [120000, 155000], arrangement: "hybrid", statusChangedDaysAgo: 0 },
  { company: "OpenAI", title: "Senior Product Engineer", status: "ongoing", currentStageIndex: 3, stages: ["HR", "Technical", "Technical", "Cultural", "Offer & Decision"], keywords: "AI product workflows • Full-stack React and Python • Product evaluation, safety, and reliability", technologies: ["React", "TypeScript", "Python", "PostgreSQL", "Kubernetes"], domain: "openai.com", salary: [145000, 190000], arrangement: "remote", statusChangedDaysAgo: 1 },
  { company: "NVIDIA", title: "Software Engineer II", status: "ongoing", currentStageIndex: 2, stages: ["HR", "Technical", "Cultural"], keywords: "GPU computing platform • Go services • Reliability engineering at global scale", technologies: ["Go", "Python", "Docker", "Kubernetes"], domain: "nvidia.com", salary: [125000, 165000], arrangement: "hybrid", statusChangedDaysAgo: 2 },
  { company: "Apple", title: "Senior macOS Software Engineer", status: "ongoing", currentStageIndex: 4, stages: ["HR", "Technical", "Technical", "Cultural", "Technical", "Offer & Decision"], keywords: "Native macOS features • Swift and AppKit • Privacy, accessibility, and system performance", technologies: ["Swift", "AppKit"], domain: "apple.com", salary: [130000, 170000], arrangement: "on_site", statusChangedDaysAgo: 0 },
  { company: "Stripe", title: "Senior Product Engineer", status: "ongoing", currentStageIndex: 1, stages: ["HR", "Technical", "Technical", "Cultural"], keywords: "Merchant payment flows • TypeScript APIs • Idempotency, edge cases, and clear product UX", technologies: ["TypeScript", "React", "Go", "PostgreSQL"], domain: "stripe.com", salary: [125000, 165000], arrangement: "remote", referral: true, statusChangedDaysAgo: 3 },
  { company: "Spotify", title: "Backend Engineer", status: "ongoing", currentStageIndex: 0, stages: ["HR", "Technical", "Cultural"], keywords: "Personalized listening • Go services • Low-latency recommendation and data pipelines", technologies: ["Go", "Python", "Kafka", "Kubernetes"], domain: "spotify.com", salary: { type: "no_min", max: 105000 }, arrangement: "hybrid", statusChangedDaysAgo: 1 },
  { company: "Microsoft", title: "Principal Software Engineer", status: "rejected", stages: ["HR", "Technical", "Technical", "Cultural"], keywords: "Azure Storage architecture • Distributed systems • Cross-team design and service reliability", technologies: ["Azure", "Go", "Kubernetes", "PostgreSQL"], domain: "microsoft.com", salary: { type: "no_max", min: 160000 }, arrangement: "hybrid", statusChangedDaysAgo: 1, lastActivityDaysAgo: 45, automaticRejectionReason: "Automatically rejected after 30 days without saved activity." },
  { company: "Datadog", title: "Senior Backend Engineer", status: "rejected", stages: ["HR", "Technical", "Cultural"], keywords: "Telemetry ingestion • Go and Kubernetes • High-throughput services and efficient queries", technologies: ["Go", "Kubernetes", "Kafka", "PostgreSQL", "Docker"], domain: "datadoghq.com", salary: [115000, 150000], arrangement: "remote", statusChangedDaysAgo: 2 },
  { company: "Meta", title: "Engineering Manager", status: "accepted", stages: ["HR", "Technical", "Cultural", "Offer & Decision"], keywords: "Product infrastructure roadmap • Engineering team development • Reliability at scale", technologies: ["React", "Python", "GraphQL", "PyTorch"], domain: "meta.com", salary: [155000, 205000], arrangement: "hybrid", statusChangedDaysAgo: 6 },
  { company: "Northstar Labs", title: "Platform Security Engineer", status: "waiting", stages: [], keywords: "Cloud platform security • Kubernetes policy • Infrastructure as code", technologies: ["Kubernetes", "Terraform", "Go"], domain: "northstar.example", salary: [105000, 135000], arrangement: "remote", createdDaysAgo: 20, statusChangedDaysAgo: 3 },
  { company: "Cedar Systems", title: "AI Platform Engineer", status: "waiting", stages: [], keywords: "LLM gateway operations • Identity and secrets • Usage observability", technologies: ["Python", "AWS", "Terraform"], domain: "cedar.example", salary: [110000, 140000], arrangement: "hybrid", createdDaysAgo: 11, statusChangedDaysAgo: 10 },
  { company: "Bluebird Analytics", title: "Application Security Engineer", status: "waiting", stages: [], keywords: "Application security • Secure development lifecycle • Vulnerability remediation", technologies: ["Python", "Go", "Docker"], domain: "bluebird.example", salary: [95000, 125000], arrangement: "on_site", createdDaysAgo: 5, statusChangedDaysAgo: 2 },
];

const demoInterviewers = ["Avery Example", "Jordan Sample", "Casey Demo", "Riley Placeholder"];
const demoRecruiters = ["Morgan Sample", "Quinn Example", "Taylor Demo", "Sam Placeholder"];
const demoContacts = ["morgan.sample@example.test", "quinn.example@example.test", "taylor.demo@example.test", "sam.placeholder@example.test"];
const meetingSlots = [
  ["09:00", "09:45"], ["10:30", "11:15"], ["13:00", "13:45"], ["14:30", "15:15"],
  ["11:00", "11:45"], ["15:30", "16:15"], ["09:30", "10:15"], ["13:30", "14:15"],
];

function makeStage(role, jobId, stageType, index, now, text) {
  const id = `${jobId}-stage-${index + 1}`;
  const current = role.status === "ongoing" && index === role.currentStageIndex;
  const completed = role.status !== "ongoing" || index < role.currentStageIndex;
  const jobNumber = Number(jobId.split("-")[1]);
  const meetingScheduled = role.status !== "ongoing" || completed || current;
  const meetingDate = new Date(now);
  const dayOffset = jobId === "demo-1" && current
    ? 0
    : completed || role.status !== "ongoing"
      ? getDemoMeetingDayOffset(id, now, "past")
      : getDemoMeetingDayOffset(id, now, "future");
  if (meetingScheduled) meetingDate.setDate(meetingDate.getDate() + dayOffset);
  const interviewerName = demoInterviewers[(jobNumber + index) % demoInterviewers.length];
  const recruiterName = demoRecruiters[(jobNumber + index) % demoRecruiters.length];
  const timeSlot = meetingSlots[(jobNumber * 2 + index) % meetingSlots.length];
  const isExternalRecruiter = role.company === "Stripe";
  const stageNotes = text.stageNotes[stageType];
  const round = role.stages.slice(0, index + 1).filter((name) => name === stageType).length;
  return {
    id, job_id: jobId, order_index: index, stage_type: stageType,
    custom_title: stageType === "Technical" && role.stages.filter((name) => name === "Technical").length > 1 ? text.roundTitle(stageType, round) : text.stageTitles[stageType],
    description: text.stageDescription(stageType, role.title, role.company),
    status: current ? "current" : completed ? "completed" : "pending",
    scheduled_at: meetingScheduled ? new Date(`${formatLocalDate(meetingDate)}T${timeSlot[0]}:00`).getTime() : null,
    meeting_date: meetingScheduled ? formatLocalDate(meetingDate) : null,
    meeting_time: meetingScheduled ? `${timeSlot[0]}–${timeSlot[1]}` : null,
    meeting_url: meetingScheduled ? `https://meet.example.com/${jobId}-stage-${index + 1}` : null,
    meeting_type: "video",
    notes: completed ? text.completedNote(stageNotes) : text.prepNote(interviewerName),
    recruiter_name: recruiterName,
    recruiter_type: isExternalRecruiter ? "external" : "internal",
    recruiter_agency: isExternalRecruiter ? text.agencies[jobNumber % text.agencies.length] : text.talentTeam(role.company),
    recruiter_contact: demoContacts[(jobNumber + index) % demoContacts.length],
    interviewers: [{ name: interviewerName, role: text.interviewerRoles[stageType], notes: text.interviewerNote(stageType) }],
    questions: (text.questions[stageType] || text.questions.Technical).slice(0, 3 + ((index + jobId.length) % 3)).map((question, qIndex) => ({
      id: `${id}-question-${qIndex + 1}`, stage_id: id, order_index: qIndex, question,
      answer_notes: completed ? text.completedAnswer(text.answers[qIndex % 3]) : text.prepAnswer(text.prepTopics[qIndex % 3]),
      is_asked: completed, created_at: now,
    })),
  };
}

/** Returns a role with its title and highlights in the language of `text`. */
function localizeRole(role, index, text) {
  const local = text.roles?.[index];
  if (!local) return { ...role, focus: role.keywords.split(" • ")[0].toLowerCase() };
  return { ...role, title: local.title, keywords: local.keywords, focus: local.focus };
}

/** Whether a title marks a senior level; it reads the English title so every language agrees. */
function isSeniorTitle(title) {
  return title.startsWith("Senior") || title.startsWith("Principal") || title.startsWith("Staff") || title.includes("Manager");
}

const now = Date.now();

/**
 * Builds the sample jobs with their wording in one language. Only text differs between languages.
 *
 * @param {string} [language] - Interface language code.
 * @returns {Array<Record<string, any>>} The sample jobs.
 */
export function buildDemoJobs(language = "en") {
  const text = textFor(language);
  const jobs = roles.map((englishRole, index) => buildDemoJob(localizeRole(englishRole, index, text), englishRole, index, text));
  assignMeetingTimes(jobs);
  return jobs;
}

function buildDemoJob(role, englishRole, index, text) {
  const recruiterName = demoRecruiters[(index + 1) % demoRecruiters.length];
  const isExternalRecruiter = role.company === "Stripe";
  const salaryMin = Array.isArray(role.salary) ? role.salary[0] : role.salary.min ?? null;
  const salaryMax = Array.isArray(role.salary) ? role.salary[1] : role.salary.max ?? null;
  const createdAt = Math.floor((now - (role.createdDaysAgo ?? index * 2) * 86400000) / 1000);
  const job = {
    id: `demo-${index + 1}`, company_name: role.company, position_title: role.title, status: role.status,
    salary_type: role.salary.type || "limited", salary_min: salaryMin, salary_max: salaryMax, salary_currency: "EUR",
    recruiter_type: isExternalRecruiter ? "external" : "internal", recruiter_name: recruiterName,
    recruiter_agency: isExternalRecruiter ? text.agencies[index % text.agencies.length] : text.talentTeam(role.company),
    recruiter_contact: demoContacts[index % demoContacts.length],
    job_post_url: null, application_url: `https://careers.${role.domain}`, avatar_seed: role.company, keyword_note: role.keywords,
    description: text.description(role),
    company_overview: text.companyOverview(role.company),
    company_domain: role.domain, interview_notes: `${text.interviewPrep[index % text.interviewPrep.length]} ${text.interviewFollowUps[index % 3]}`,
    reasons_to_change: text.reasonsToChange(role.focus),
    experience_notes: text.experienceNotes(role.focus, role.title, isSeniorTitle(englishRole.title)),
    expected_salary: role.salary.type === "no_min"
      ? text.salaryUpTo(role.salary.max)
      : role.salary.type === "no_max"
        ? text.salaryFrom(role.salary.min)
        : text.salaryRange(role.salary[0], role.salary[1]),
    work_arrangement: role.arrangement, employment_type: "permanent", is_referral: Boolean(role.referral),
    order_index: index,
    application_sent_date: role.status === "waiting" && !role.referral ? formatLocalDate(new Date(createdAt * 1000)) : undefined,
    created_at: createdAt,
    status_changed_at: Math.floor((now - (role.statusChangedDaysAgo ?? index) * 86400000) / 1000),
    history: [
      { occurred_at: createdAt, action: "created" },
      ...(Math.floor((now - (role.statusChangedDaysAgo ?? index) * 86400000) / 1000) > createdAt
        ? [{ occurred_at: Math.floor((now - (role.statusChangedDaysAgo ?? index) * 86400000) / 1000), action: `moved to ${role.status}` }]
        : []),
    ],
    updated_at: Math.floor(now / 1000),
    last_activity_at: Math.floor((now - (role.lastActivityDaysAgo ?? 0) * 86400000) / 1000),
    automatic_rejection_reason: role.automaticRejectionReason || "",
    attachments: [], interviewers: [], stages: [], technologies: role.technologies.map(technologyRecord),
  };
  job.stages = role.stages.map((stageType, stageIndex) => makeStage(role, job.id, stageType, stageIndex, now, text));
  const currentIndex = job.stages.findIndex((stage) => stage.status === "current");
  const current = job.stages[currentIndex];
  job.current_stage_title = current ? current.custom_title || current.stage_type : undefined;
  job.current_stage_title_is_type = Boolean(current && !current.custom_title);
  job.current_stage_index = current ? currentIndex + 1 : undefined;
  job.total_stages_count = job.stages.length;
  return job;
}

const meetingTimes = ["09:00–09:45", "10:00–10:45", "11:00–11:45", "13:00–13:45", "14:00–14:45", "15:00–15:45", "16:00–16:45"];
function assignMeetingTimes(jobs) {
  const scheduledStagesByDate = new Map();
  for (const job of jobs) {
    for (const stage of job.stages) {
      if (!stage.meeting_date) continue;
      const stagesOnDate = scheduledStagesByDate.get(stage.meeting_date) || [];
      stagesOnDate.push(stage);
      scheduledStagesByDate.set(stage.meeting_date, stagesOnDate);
    }
  }
  for (const stages of scheduledStagesByDate.values()) {
    stages.sort((a, b) => a.job_id.localeCompare(b.job_id) || a.order_index - b.order_index);
    stages.forEach((stage, index) => {
      stage.meeting_time = meetingTimes[index % meetingTimes.length];
      const startTime = stage.meeting_time.split("–")[0];
      stage.scheduled_at = new Date(`${stage.meeting_date}T${startTime}:00`).getTime();
    });
  }
}

/** The sample jobs as first saved, in English. */
export const initialJobs = buildDemoJobs("en");

const historicalRecords = [
  { company: "Morrow Design Co.", role: "UX Designer", status: "accepted", date: "2021-06-03", arrangement: "remote", salaryMin: 65000, salaryMax: 82000 },
  { company: "Cinder Analytics", role: "Data Analyst", status: "rejected", date: "2021-06-07", arrangement: "hybrid", salaryMin: 58000, salaryMax: 74000 },
  { company: "Saffron Labs", role: "Product Manager", status: "rejected", date: "2021-06-09", arrangement: "remote", salaryMin: 72000, salaryMax: 91000 },
  { company: "Kestrel Mobility", role: "iOS Engineer", status: "rejected", date: "2021-06-11", arrangement: "hybrid", salaryMin: 78000, salaryMax: 98000 },
  { company: "Lattice Grove", role: "Customer Success Engineer", status: "rejected", date: "2021-06-14", arrangement: "remote", salaryMin: 62000, salaryMax: 80000 },
  { company: "Orbit Foundry", role: "QA Automation Engineer", status: "rejected", date: "2021-06-16", arrangement: "hybrid", salaryMin: 60000, salaryMax: 79000 },
  { company: "Stonebridge Health", role: "Product Designer", status: "rejected", date: "2021-06-18", arrangement: "remote", salaryMin: 68000, salaryMax: 86000 },
  { company: "Redfern Commerce", role: "Backend Engineer", status: "rejected", date: "2021-06-21", arrangement: "hybrid", salaryMin: 76000, salaryMax: 96000 },
  { company: "Silverpath Energy", role: "Data Engineer", status: "rejected", date: "2021-06-24", arrangement: "remote", salaryMin: 74000, salaryMax: 94000 },
  { company: "Atlas Harbor", role: "Technical Writer", status: "rejected", date: "2021-06-28", arrangement: "hybrid", salaryMin: 54000, salaryMax: 70000 },
];

/**
 * Builds the closed sample jobs of the June 2021 search period. Each copies a current sample job's notes.
 *
 * @param {{ language?: string, periodId?: string, firstOrderIndex?: number }} [options] - Wording language, period, and first board position.
 * @returns {Array<Record<string, any>>} The historical sample jobs.
 */
export function buildHistoricalDemoJobs({ language = "en", periodId = "demo-period-june-2021", firstOrderIndex = 0 } = {}) {
  const text = textFor(language);
  const baseJobs = buildDemoJobs(language);
  return historicalRecords.map((record, index) => {
    const job = JSON.parse(JSON.stringify(baseJobs[index % baseJobs.length]));
    const role = text.historicalRoles?.[index] || record.role;
    const timestamp = Math.floor(Date.parse(`${record.date}T12:00:00Z`) / 1000);
    Object.assign(job, {
      id: `demo-period-june-2021-${index + 1}`,
      company_name: record.company,
      position_title: role,
      status: record.status,
      status_changed_at: timestamp,
      created_at: timestamp,
      updated_at: timestamp,
      last_activity_at: timestamp,
      automatic_rejection_reason: "",
      search_period_id: periodId,
      order_index: firstOrderIndex + index,
      avatar_seed: record.company,
      job_post_url: `https://careers.${record.company.toLowerCase().replaceAll(" ", "-")}.example.test`,
      company_domain: `${record.company.toLowerCase().replaceAll(" ", "-")}.example.test`,
      description: text.historicalDescription(role, record.company),
      company_overview: text.historicalOverview(record.company),
      keyword_note: text.historicalKeywords(role),
      expected_salary: text.salaryRange(record.salaryMin, record.salaryMax),
      salary_min: record.salaryMin,
      salary_max: record.salaryMax,
      work_arrangement: record.arrangement,
      is_referral: false,
      application_sent_date: record.date,
      stages: [],
      interviewers: [],
      technologies: [],
      attachments: [],
      total_stages_count: 0,
    });
    delete job.current_stage_title;
    delete job.current_stage_title_is_type;
    delete job.current_stage_index;
    return job;
  });
}
