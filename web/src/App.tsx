export type CrossExamView = {
  verdict: string;
  reviewer_model: string;
  reasoning: string;
};

export function CrossExamNotes({ exam }: { exam?: CrossExamView | null }) {
  if (!exam) {
    return null;
  }
  return (
    <section>
      <h2>Cross-exam</h2>
      <p>
        {exam.verdict} · {exam.reviewer_model}
      </p>
      {exam.reasoning ? <p>{exam.reasoning}</p> : <p>No notes</p>}
    </section>
  );
}

export function App() {
  return (
    <main>
      <h1>Zeroth</h1>
      <p>
        Local, single-player control plane. Stage 1 has no deployment story.
        Multiplayer is stage 2.
      </p>
      <CrossExamNotes exam={null} />
    </main>
  );
}
