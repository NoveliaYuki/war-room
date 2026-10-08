const names = [
  "React", "TypeScript", "JavaScript", "Python", "Go", "Swift", "AppKit", "Kubernetes", "PostgreSQL", "Azure", "AWS", "Terraform", "Kafka", "GraphQL", "TensorFlow", "PyTorch", "Docker",
];

const idFor = (name) => `tech-${name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, "")}`;

/** Canonical technology catalog for the fictional demo opportunities. */
export const initialTechnologies = names.map((name) => ({
  id: idFor(name), name, aliases: name === "Kubernetes" ? ["K8s"] : [],
}));

export function technologyRecord(name) {
  return { id: idFor(name), name };
}
