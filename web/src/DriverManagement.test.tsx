import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DriverManagement } from "./DriverManagement";

const companyID="aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa";
const operationID="aaaaaaaa-aaaa-aaaa-aaaa-000000000001";
const personID="aaaaaaaa-aaaa-aaaa-aaaa-100000000003";
const props={companyID,operations:[{id:operationID,name:"Drayage"}],onChanged:vi.fn(),onInvite:vi.fn(),canInvite:()=>true};
const driver={person_id:personID,identifier:"driver@example.test",legal_name:"Test Driver",phone:"+56912345678",status:"ACTIVE",version:1,operation_ids:[operationID],enrollment_status:"ACTIVE"};
const json=(body:unknown,status=200)=>new Response(JSON.stringify(body),{status,headers:{"Content-Type":"application/json"}});
afterEach(()=>vi.restoreAllMocks());

describe("company driver management",()=>{
 it("hides registration and mutations when permission is denied",async()=>{
  vi.spyOn(globalThis,"fetch").mockResolvedValue(json({},403));
  render(<DriverManagement {...props}/>);
  expect(await screen.findByRole("alert")).toHaveTextContent("No tienes permiso");
  expect(screen.queryByRole("button",{name:"Registrar conductor"})).not.toBeInTheDocument();
 });
 it("registers a named driver with operations, CSRF and an idempotency key without a password",async()=>{
  document.cookie="tp_csrf=test-csrf";
  const fetch=vi.spyOn(globalThis,"fetch").mockImplementation(async(_input,init)=>json(init?.method==="POST"?{status:"CREATED"}:{status:"OK",drivers:[]}));
  const changed=vi.fn();
  render(<DriverManagement {...props} onChanged={changed}/>);
  fireEvent.click(await screen.findByRole("button",{name:"Registrar conductor"}));
  for(const [label,value] of [["Nombre","Test"],["Apellidos","Driver"],["Identificador de acceso","test@example.test"],["Teléfono de contacto","+56912345678"]]) fireEvent.change(screen.getByLabelText(label),{target:{value}});
  fireEvent.click(screen.getByRole("checkbox",{name:"Drayage"}));
  fireEvent.click(screen.getByRole("button",{name:"Guardar conductor"}));
  await screen.findByText(/Conductor registrado/);
  const mutation=fetch.mock.calls.find(([,init])=>init?.method==="POST");
  expect(JSON.parse(String(mutation?.[1]?.body))).toEqual({given_name:"Test",family_name:"Driver",identifier:"test@example.test",phone:"+56912345678",operation_ids:[operationID]});
  expect(new Headers(mutation?.[1]?.headers).get("Idempotency-Key")).toBeTruthy();
  expect(new Headers(mutation?.[1]?.headers).get("X-CSRF-Token")).toBe("test-csrf");
  expect(changed).toHaveBeenCalledOnce();
 });
 it("displays existing drivers and keeps invitation permission independent",async()=>{
  vi.spyOn(globalThis,"fetch").mockResolvedValue(json({status:"OK",drivers:[driver]}));
  const invite=vi.fn();const view=render(<DriverManagement {...props} onInvite={invite} canInvite={()=>false}/>);
  await screen.findByText("Test Driver");
  expect(screen.queryByText(companyID)).not.toBeInTheDocument();
  expect(screen.queryByRole("button",{name:"Preparar invitación por WhatsApp"})).not.toBeInTheDocument();
  view.rerender(<DriverManagement {...props} onInvite={invite}/>);
  fireEvent.click(screen.getByRole("button",{name:"Preparar invitación por WhatsApp"}));
  expect(invite).toHaveBeenCalledWith(personID);
 });
 it("suspends only the company relationship with an explicit version",async()=>{
  vi.spyOn(window,"confirm").mockReturnValue(true);
  const fetch=vi.spyOn(globalThis,"fetch").mockImplementation(async(_input,init)=>json(init?.method==="PATCH"?{status:"UPDATED"}:{status:"OK",drivers:[driver]}));
  render(<DriverManagement {...props}/>);
  fireEvent.click(await screen.findByRole("button",{name:"Suspender en empresa"}));
  await waitFor(()=>expect(fetch.mock.calls.some(([,init])=>init?.method==="PATCH")).toBe(true));
  const mutation=fetch.mock.calls.find(([,init])=>init?.method==="PATCH");
  expect(JSON.parse(String(mutation?.[1]?.body))).toMatchObject({person_id:personID,version:1,status:"SUSPENDED",operation_ids:[operationID]});
 });
 it("reports a review requirement without exposing another company's identity",async()=>{
  vi.spyOn(globalThis,"fetch").mockImplementation(async(_input,init)=>json(init?.method==="POST"?{status:"REVIEW_REQUIRED"}:{status:"OK",drivers:[]},init?.method==="POST"?202:200));
  render(<DriverManagement {...props}/>);
  fireEvent.click(await screen.findByRole("button",{name:"Registrar conductor"}));
  for(const [label,value] of [["Nombre","Test"],["Apellidos","Driver"],["Identificador de acceso","existing@example.test"],["Teléfono de contacto","+56912345678"]]) fireEvent.change(screen.getByLabelText(label),{target:{value}});
  fireEvent.click(screen.getByRole("button",{name:"Guardar conductor"}));
  expect(await screen.findByRole("status")).toHaveTextContent("La vinculación requiere autorización adicional");
 });
});
