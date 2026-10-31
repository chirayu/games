export interface HealthResponse {
  status: string;
}

export async function getHealth(): Promise<HealthResponse> {
  const response = await fetch("/healthz", {
    headers: { Accept: "application/json" },
  });

  if (!response.ok) {
    throw new Error(`health check failed with status ${response.status}`);
  }

  const body = (await response.json()) as { data: HealthResponse };
  return body.data;
}
