"use client";
export default function ErrorPage({ reset }: { reset: () => void }) { return <main id="main" className="page"><h1>This view couldn’t load.</h1><p>Your repository has not been changed by this page error. Reload the view to try again.</p><button className="button primary" onClick={reset}>Try again</button></main>; }
