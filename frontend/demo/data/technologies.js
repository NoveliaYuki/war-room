const names = [
  "React", "TypeScript", "JavaScript", "Python", "Go", "Swift", "AppKit", "Kubernetes", "PostgreSQL", "Azure", "AWS", "Terraform", "Kafka", "GraphQL", "TensorFlow", "PyTorch", "Docker",
];

const aliasesByName = {
  TypeScript: ["TS"],
  JavaScript: ["JS"],
  Kubernetes: ["K8s"],
};

const idFor = (name) => `tech-${name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, "")}`;

/** Canonical technology catalog for the fictional demo opportunities. */
export const initialTechnologies = names.map((name) => ({
  id: idFor(name), name, aliases: aliasesByName[name] || [],
}));

export function technologyRecord(name) {
  return { id: idFor(name), name };
}
