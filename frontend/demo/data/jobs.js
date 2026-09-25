/** Browser-only sample records for the interactive demo. */
const stageQuestions = {
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
};

const roles = [
  { company: "Google", title: "Senior Frontend Engineer", status: "ongoing", currentStageIndex: 1, stages: ["HR", "Technical", "Cultural", "Offer & Decision"], keywords: "Accessible high-traffic search UI • React architecture • Measurable performance and Core Web Vitals", domain: "google.com", salary: [120000, 155000], arrangement: "hybrid" },
  { company: "OpenAI", title: "Senior Product Engineer", status: "ongoing", currentStageIndex: 3, stages: ["HR", "Technical", "Technical", "Cultural", "Offer & Decision"], keywords: "AI product workflows • Full-stack React and Python • Product evaluation, safety, and reliability", domain: "openai.com", salary: [145000, 190000], arrangement: "remote" },
  { company: "Factorial", title: "Software Engineer II", status: "ongoing", currentStageIndex: 2, stages: ["HR", "Technical", "Cultural"], keywords: "Multi-tenant HR workflows • Go services • Platform reliability and collaborative delivery", domain: "factorialhr.com", salary: [65000, 85000], arrangement: "hybrid" },
  { company: "Apple", title: "Senior macOS Software Engineer", status: "ongoing", currentStageIndex: 4, stages: ["HR", "Technical", "Technical", "Cultural", "Technical", "Offer & Decision"], keywords: "Native macOS features • Swift and AppKit • Privacy, accessibility, and system performance", domain: "apple.com", salary: [130000, 170000], arrangement: "on_site" },
  { company: "Stripe", title: "Senior Product Engineer", status: "ongoing", currentStageIndex: 1, stages: ["HR", "Technical", "Technical", "Cultural"], keywords: "Merchant payment flows • TypeScript APIs • Idempotency, edge cases, and clear product UX", domain: "stripe.com", salary: [125000, 165000], arrangement: "remote", referral: true },
  { company: "Spotify", title: "Backend Engineer", status: "ongoing", currentStageIndex: 0, stages: ["HR", "Technical", "Cultural"], keywords: "Personalized listening • Go services • Low-latency recommendation and data pipelines", domain: "spotify.com", salary: { type: "no_min", max: 105000 }, arrangement: "hybrid" },
  { company: "Microsoft", title: "Principal Software Engineer", status: "rejected", stages: ["HR", "Technical", "Technical", "Cultural"], keywords: "Azure Storage architecture • Distributed systems • Cross-team design and service reliability", domain: "microsoft.com", salary: { type: "no_max", min: 160000 }, arrangement: "hybrid" },
  { company: "Datadog", title: "Senior Backend Engineer", status: "rejected", stages: ["HR", "Technical", "Cultural"], keywords: "Telemetry ingestion • Go and Kubernetes • High-throughput services and efficient queries", domain: "datadoghq.com", salary: [115000, 150000], arrangement: "remote" },
  { company: "Meta", title: "Engineering Manager", status: "accepted", stages: ["HR", "Technical", "Cultural", "Offer & Decision"], keywords: "Product infrastructure roadmap • Engineering team development • Reliability at scale", domain: "meta.com", salary: [155000, 205000], arrangement: "hybrid" },
];

const demoInterviewers = ["Avery Example", "Jordan Sample", "Casey Demo", "Riley Placeholder"];
const demoRecruiters = ["Morgan Sample", "Quinn Example", "Taylor Demo", "Sam Placeholder"];
const demoContacts = ["morgan.sample@example.test", "quinn.example@example.test", "taylor.demo@example.test", "sam.placeholder@example.test"];
const demoAgencies = ["Northstar Talent (sample)", "Bridgeway Search (sample)", "Juniper People (sample)"];
const meetingSlots = [
  ["09:00", "09:45"], ["10:30", "11:15"], ["13:00", "13:45"], ["14:30", "15:15"],
  ["11:00", "11:45"], ["15:30", "16:15"], ["09:30", "10:15"], ["13:30", "14:15"],
];

function formatLocalDate(date) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function makeStage(role, jobId, stageType, index, now) {
  const id = `${jobId}-stage-${index + 1}`;
  const current = role.status === "ongoing" && index === role.currentStageIndex;
  const completed = role.status !== "ongoing" || index < role.currentStageIndex;
  const jobNumber = Number(jobId.split("-")[1]);
  const meetingScheduled = role.status !== "ongoing" || completed || current;
  const meetingDate = new Date(now);
  const dayOffset = role.status === "ongoing"
    ? completed ? -(jobNumber + index + 1) : (jobNumber * 2) % 9 + 1
    : -(jobNumber + index + 2);
  if (meetingScheduled) meetingDate.setDate(meetingDate.getDate() + dayOffset);
  const interviewerName = demoInterviewers[(jobNumber + index) % demoInterviewers.length];
  const recruiterName = demoRecruiters[(jobNumber + index) % demoRecruiters.length];
  const timeSlot = meetingSlots[(jobNumber * 2 + index) % meetingSlots.length];
  const isExternalRecruiter = role.company === "Stripe";
  const stageNotes = {
    HR: "The interviewer outlined the team's priorities and asked about the candidate's recent work.",
    Technical: "Discussed design trade-offs, failure handling, and how the approach would scale.",
    Cultural: "Compared collaboration styles and shared examples of giving and receiving feedback.",
    "Offer & Decision": "Reviewed role expectations, success measures, and the sample decision timeline.",
  }[stageType];
  return {
    id, job_id: jobId, order_index: index, stage_type: stageType,
    custom_title: stageType === "Technical" && role.stages.filter((name) => name === "Technical").length > 1 ? `${stageType} Round ${role.stages.slice(0, index + 1).filter((name) => name === stageType).length}` : stageType,
    description: `${stageType} conversation for the ${role.title} role at ${role.company}.`,
    status: current ? "current" : completed ? "completed" : "pending",
    scheduled_at: meetingScheduled ? new Date(`${formatLocalDate(meetingDate)}T${timeSlot[0]}:00`).getTime() : null,
    meeting_date: meetingScheduled ? formatLocalDate(meetingDate) : null,
    meeting_time: meetingScheduled ? `${timeSlot[0]}–${timeSlot[1]}` : null,
    meeting_url: meetingScheduled ? `https://meet.example.com/${jobId}-stage-${index + 1}` : null,
    meeting_type: "video",
    notes: completed
      ? `Demo conversation note: ${stageNotes}`
      : `Demo prep note: Ask ${interviewerName} about the team's priorities, success measures, and next steps.`,
    recruiter_name: recruiterName,
    recruiter_type: isExternalRecruiter ? "external" : "internal",
    recruiter_agency: isExternalRecruiter ? demoAgencies[jobNumber % demoAgencies.length] : `${role.company} Talent Acquisition`,
    recruiter_contact: demoContacts[(jobNumber + index) % demoContacts.length],
    interviewers: [{ name: interviewerName, role: {
      HR: "Talent Partner",
      Technical: "Senior Engineer",
      Cultural: "Engineering Manager",
      "Offer & Decision": "Hiring Manager",
    }[stageType], notes: `Fictional ${stageType.toLowerCase()} interviewer for this sample process.` }],
    questions: (stageQuestions[stageType] || stageQuestions.Technical).slice(0, 3 + ((index + jobId.length) % 3)).map((question, qIndex) => ({
      id: `${id}-question-${qIndex + 1}`, stage_id: id, order_index: qIndex, question,
      answer_notes: completed
        ? `Sample response: ${["Shared a concise example with measurable impact.", "Explained the trade-offs and how the team aligned.", "Connected the decision to reliability and customer needs."][qIndex % 3]}`
        : `Prep: have a concise example ready about ${["measurable impact", "trade-offs and alignment", "reliability and customer needs"][qIndex % 3]}.`,
      is_asked: completed, created_at: now,
    })),
  };
}

const now = Date.now();
const interviewPrep = [
  "Prepare concise examples with context, decisions, and measurable outcomes.",
  "Practice explaining technical trade-offs in plain language and connecting them to user or business impact.",
  "Bring thoughtful questions about the team, role expectations, and how success is measured.",
];
export const initialJobs = roles.map((role, index) => {
  const recruiterName = demoRecruiters[(index + 1) % demoRecruiters.length];
  const isExternalRecruiter = role.company === "Stripe";
  const salaryMin = Array.isArray(role.salary) ? role.salary[0] : role.salary.min ?? null;
  const salaryMax = Array.isArray(role.salary) ? role.salary[1] : role.salary.max ?? null;
  const job = {
    id: `demo-${index + 1}`, company_name: role.company, position_title: role.title, status: role.status,
    salary_type: role.salary.type || "limited", salary_min: salaryMin, salary_max: salaryMax, salary_currency: "EUR",
    recruiter_type: isExternalRecruiter ? "external" : "internal", recruiter_name: recruiterName,
    recruiter_agency: isExternalRecruiter ? demoAgencies[index % demoAgencies.length] : `${role.company} Talent Acquisition`,
    recruiter_contact: demoContacts[index % demoContacts.length],
    job_post_url: `https://careers.${role.domain}`, avatar_seed: role.company, keyword_note: role.keywords,
    description: `${role.title} on ${role.company}'s ${role.arrangement.replace("_", " ")} team. The role focuses on ${role.keywords.toLowerCase()}. Responsibilities include designing and shipping reliable software, reviewing technical proposals, partnering with product and design, and improving service quality. The team works across engineering, product, and design; success is measured through customer outcomes, delivery quality, and operational health.\n\nFictional sample opportunity for the War Room demo; this is not an active job listing.`,
    company_overview: `${role.company} is shown as a sample employer in this fictional interview pipeline. Company identity and logo are real; the role details, recruiter, interviewers, and hiring activity are illustrative demo data.`,
    company_domain: role.domain, interview_notes: `${interviewPrep[index % interviewPrep.length]} ${["Ask how the team plans work across a typical quarter.", "Clarify the balance between hands-on delivery and technical leadership.", "Ask what the first 90 days would look like for this role."][index % 3]}`,
    reasons_to_change: `Looking for broader ownership of ${role.keywords.split(" • ")[0].toLowerCase()}, with a team that values thoughtful engineering and clear customer impact. The move would build on current experience while providing room to grow in scope.`,
    experience_notes: `Relevant sample experience: delivered production features related to ${role.keywords.split(" • ")[0].toLowerCase()}, partnered with product and design to scope work, and used monitoring and user feedback to improve reliability. For the ${role.title} level, examples emphasize ${role.title.startsWith("Senior") || role.title.startsWith("Principal") || role.title.startsWith("Staff") || role.title.includes("Manager") ? "technical direction, mentoring, and cross-team outcomes" : "independent feature ownership, code quality, and effective collaboration"}.`,
    expected_salary: role.salary.type === "no_min"
      ? `Up to €${role.salary.max.toLocaleString()}`
      : role.salary.type === "no_max"
        ? `From €${role.salary.min.toLocaleString()}`
        : `€${role.salary[0].toLocaleString()}–€${role.salary[1].toLocaleString()}`,
    work_arrangement: role.arrangement, employment_type: "permanent", is_referral: Boolean(role.referral),
    order_index: index, created_at: now - index * 86400000, updated_at: now,
    attachments: [], interviewers: [], stages: [],
  };
  job.stages = role.stages.map((stageType, stageIndex) => makeStage(role, job.id, stageType, stageIndex, now));
  const currentIndex = job.stages.findIndex((stage) => stage.status === "current");
  const current = job.stages[currentIndex];
  job.current_stage_title = current ? current.custom_title || current.stage_type : undefined;
  job.current_stage_index = current ? currentIndex + 1 : undefined;
  job.total_stages_count = job.stages.length;
  return job;
});

const meetingTimes = ["09:00–09:45", "10:00–10:45", "11:00–11:45", "13:00–13:45", "14:00–14:45", "15:00–15:45", "16:00–16:45"];
const scheduledStagesByDate = new Map();
for (const job of initialJobs) {
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
