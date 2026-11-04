export type DemoOperation = {
  id: string;
  name: string;
};

export type DemoCompany = {
  id: string;
  name: string;
  operations: DemoOperation[];
};

// This is an explicit synthetic catalog for the client demo. There is no
// tenant-discovery endpoint in this phase, so these values must remain limited
// to the fixture and must not be presented as a platform-wide directory.
export const demoCompanies: DemoCompany[] = [
  {
    id: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
    name: "Manual Acme Logistics",
    operations: [
      { id: "aaaaaaaa-aaaa-aaaa-aaaa-000000000001", name: "Drayage" },
    ],
  },
];

export const findDemoCompany = (id: string): DemoCompany | undefined =>
  demoCompanies.find((company) => company.id === id);

export const findDemoOperation = (id: string | null): DemoOperation | undefined => {
  if (!id) return undefined;
  return demoCompanies.flatMap((company) => company.operations).find((operation) => operation.id === id);
};
