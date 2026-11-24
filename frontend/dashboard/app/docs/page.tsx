import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "API Docs | CryptoEngine",
  description: "FastAPI Swagger UI",
};

export default function DocsPage() {
  return (
    <div className="-mx-4 -my-6 h-screen sm:-mx-6 lg:-mx-10 lg:-my-8">
      <iframe
        src="http://localhost:8000/docs"
        title="API Documentation (Swagger UI)"
        className="h-full w-full border-0"
      />
    </div>
  );
}
