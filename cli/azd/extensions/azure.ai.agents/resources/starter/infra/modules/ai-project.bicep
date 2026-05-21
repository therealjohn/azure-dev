targetScope = 'resourceGroup'

@description('Tags for all resources')
param tags object = {}

@description('Location for resources')
param location string

@description('Name of the AI Foundry project')
param foundryProjectName string

@description('Name of the AI Foundry account that owns this project. The account must be in the same resource group as this deployment.')
param foundryAccountName string

@description('Connections to create from azure.yaml')
param connections array = []

@secure()
@description('Credentials map for connections: { "conn-name": { "key": "..." } }')
param connectionCredentials object = {}

@description('Developer principal ID for RBAC')
param principalId string

@description('Developer principal type')
param principalType string

@description('Use an existing Foundry project instead of creating one')
param useExistingFoundryProject bool = false

@description('Provision Application Insights + Log Analytics and connect them to the project. Defaults true. Only affects NEW projects; existing projects keep their existing monitoring wiring.')
param enableMonitoring bool = true

@description('Existing App Insights connection string (for existing projects)')
param existingAppInsightsConnectionString string = ''

@description('Existing App Insights resource ID (for existing projects)')
param existingAppInsightsResourceId string = ''

@description('Network mode (from main.bicep). Reserved for future use; the project-scoped capability host is created by a separate module (project-cap-host.bicep) called by main.bicep when networkMode=byo-vnet-standard.')
@allowed([
  'none'
  'managed'
  'byo-vnet'
  'byo-vnet-standard'
])
param networkMode string = 'none'

var resourceToken = uniqueString(subscription().id, resourceGroup().id, location)
var isStandard = networkMode == 'byo-vnet-standard'

// ─────────────────────────────────────────────────────────────────────
// Account references (same RG)
// ─────────────────────────────────────────────────────────────────────

resource foundryAccount 'Microsoft.CognitiveServices/accounts@2026-03-01' existing = {
  name: foundryAccountName

  resource existingProject 'projects' existing = if (useExistingFoundryProject) {
    name: foundryProjectName
  }
}

// ─────────────────────────────────────────────────────────────────────
// Project (new)
// ─────────────────────────────────────────────────────────────────────

resource newProject 'Microsoft.CognitiveServices/accounts/projects@2026-03-01' = if (!useExistingFoundryProject) {
  parent: foundryAccount
  name: foundryProjectName
  location: location
  identity: { type: 'SystemAssigned' }
  properties: {
    description: '${foundryProjectName} Project'
    displayName: '${foundryProjectName}Project'
  }
}

// ─────────────────────────────────────────────────────────────────────
// Monitoring (new project only)
// ─────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────
// Monitoring (new project only; gated by enableMonitoring)
// ─────────────────────────────────────────────────────────────────────

var provisionMonitoring = !useExistingFoundryProject && enableMonitoring

resource logAnalytics 'Microsoft.OperationalInsights/workspaces@2025-07-01' = if (provisionMonitoring) {
  name: 'logs-${resourceToken}'
  location: location
  tags: tags
  properties: {
    retentionInDays: 30
    features: { searchVersion: 1 }
    sku: { name: 'PerGB2018' }
  }
}

resource appInsights 'Microsoft.Insights/components@2020-02-02' = if (provisionMonitoring) {
  name: 'appi-${resourceToken}'
  location: location
  tags: tags
  kind: 'web'
  properties: {
    Application_Type: 'web'
    #disable-next-line BCP318
    WorkspaceResourceId: logAnalytics.id
  }
}

resource appInsightsConnection 'Microsoft.CognitiveServices/accounts/projects/connections@2026-03-01' = if (provisionMonitoring) {
  #disable-next-line BCP318
  parent: newProject
  name: 'appi-${resourceToken}'
  properties: {
    category: 'AppInsights'
    #disable-next-line BCP318
    target: appInsights.id
    authType: 'ApiKey'
    isSharedToAll: true
    #disable-next-line BCP318
    credentials: { key: appInsights.properties.ConnectionString }
    metadata: {
      ApiType: 'Azure'
      #disable-next-line BCP318
      ResourceId: appInsights.id
    }
  }
}

// Log Analytics Reader for project managed identity (enables trace evaluations)
resource logAnalyticsReaderRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = if (provisionMonitoring) {
  #disable-next-line BCP318
  scope: appInsights
  #disable-next-line BCP318
  name: guid(appInsights.id, newProject.name, '73c42c96-874c-492b-b04d-ab87d138a893')
  properties: {
    #disable-next-line BCP318
    principalId: newProject.identity.principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '73c42c96-874c-492b-b04d-ab87d138a893')
  }
}

// ─────────────────────────────────────────────────────────────────────
// RBAC — Foundry User for the developer on the NEW project only
// (Foundry User was previously named "Azure AI User"; role ID unchanged.)
// Existing projects: the caller already has access to them (otherwise they
// couldn't have selected the project), so the template does not re-assign
// roles to existing projects.
// ─────────────────────────────────────────────────────────────────────

var foundryUserRoleId = '53ca6127-db72-4b80-b1b0-d745d6d5456d'

resource newProjectRbac 'Microsoft.Authorization/roleAssignments@2022-04-01' = if (!useExistingFoundryProject) {
  #disable-next-line BCP318
  scope: newProject
  name: guid(subscription().id, resourceGroup().id, principalId, foundryUserRoleId, foundryProjectName)
  properties: {
    principalId: principalId
    principalType: principalType
    roleDefinitionId: resourceId('Microsoft.Authorization/roleDefinitions', foundryUserRoleId)
  }
}

// ─────────────────────────────────────────────────────────────────────
// Project-scoped capability host is created by a SEPARATE module
// (project-cap-host.bicep) from main.bicep, AFTER project connections to
// Cosmos / Storage / AI Search are created and pre-cap-host RBAC is in place.
// Avoids a chicken-and-egg cycle: data modules need projectPrincipalId; the
// cap host needs the data-resource connections to exist on the project.
// ─────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────
// Connections from azure.yaml (works for both new and existing)
// ─────────────────────────────────────────────────────────────────────

module foundryConnections './connection.bicep' = [for (conn, i) in connections: {
  name: 'connection-${conn.name}'
  params: {
    foundryAccountName: foundryAccountName
    foundryProjectName: foundryProjectName
    connectionConfig: conn
    credentials: connectionCredentials[?conn.name] ?? {}
  }
  // Mutually exclusive: exactly one of newProject / existingProject is deployed
  // per useExistingFoundryProject. Conditional resources that are NOT deployed are
  // safely no-op in dependsOn arrays at runtime (ARM ignores them).
  dependsOn: [
    newProject
    foundryAccount::existingProject
  ]
}]

// ─────────────────────────────────────────────────────────────────────
// Outputs (existing-vs-new short-circuit ternary; BCP318-suppressed)
// ─────────────────────────────────────────────────────────────────────

output projectName string = foundryProjectName
#disable-next-line BCP318
output projectId string = useExistingFoundryProject ? foundryAccount::existingProject.id : newProject.id
#disable-next-line BCP318
output projectPrincipalId string = useExistingFoundryProject ? foundryAccount::existingProject.identity.principalId : newProject.identity.principalId
#disable-next-line BCP318
output projectEndpoint string = useExistingFoundryProject ? foundryAccount::existingProject.properties.endpoints['AI Foundry API'] : newProject.properties.endpoints['AI Foundry API']
// Monitoring outputs:
//   - useExistingFoundryProject: pass through caller-supplied existing values
//   - new project + monitoring disabled: empty strings
//   - new project + monitoring enabled: pulled from the deployed resources
// Force-unwrap (`!`) on appInsights properties is safe: only reached when
// provisionMonitoring is true, which is the condition that creates appInsights.
output appInsightsConnectionString string = useExistingFoundryProject
  ? existingAppInsightsConnectionString
  : (provisionMonitoring ? appInsights!.properties.ConnectionString : '')
output appInsightsResourceId string = useExistingFoundryProject
  ? existingAppInsightsResourceId
  : (provisionMonitoring ? appInsights!.id : '')
output connectionIds array = [for (conn, i) in connections: {
  name: foundryConnections[i].outputs.connectionName
  id: foundryConnections[i].outputs.connectionId
}]
// Workspace GUID derived from internalId — used by Standard post-cap-host RBAC modules.
// project.properties.internalId is a string GUID per sample 15. The Bicep type defs
// don't expose this property (BCP053) and the `existing` project reference may be
// null at compile time (BCP318); both are runtime-safe because the access is gated
// by `isStandard` and `useExistingFoundryProject` matching the deployed branch.
#disable-next-line BCP318 BCP053
output projectWorkspaceId string = isStandard ? (useExistingFoundryProject ? foundryAccount::existingProject!.properties.internalId : newProject!.properties.internalId) : ''
