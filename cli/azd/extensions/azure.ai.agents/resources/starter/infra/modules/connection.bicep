targetScope = 'resourceGroup'

@description('AI Services account name')
param foundryAccountName string

@description('AI project name')
param foundryProjectName string

// ConnectionConfig is a strongly-typed shape for a Foundry project connection.
// All optional fields are forwarded to the underlying connection resource when
// the caller supplies them; omitted fields are not emitted in the ARM payload.
//
// Adopted from JFolberth/simple-hosted-agent-responses to provide authoring-
// time IntelliSense and validation for callers across the starter template.
type ConnectionConfig = {
  @description('Name of the connection.')
  name: string

  @description('Category of the connection (e.g. ContainerRegistry, AzureStorageAccount, CognitiveSearch, AzureOpenAI, AppInsights, CosmosDB, GroundingWithBingSearch).')
  category: string

  @description('Target endpoint or URL for the connection.')
  target: string

  @description('Authentication type.')
  authType:
    | 'AAD'
    | 'AccessKey'
    | 'AccountKey'
    | 'AgenticIdentity'
    | 'ApiKey'
    | 'CustomKeys'
    | 'ManagedIdentity'
    | 'None'
    | 'OAuth2'
    | 'PAT'
    | 'ProjectManagedIdentity'
    | 'SAS'
    | 'ServicePrincipal'
    | 'UserEntraToken'
    | 'UsernamePassword'

  @description('Optional. Whether the connection is shared to all users (defaults to true).')
  isSharedToAll: bool?

  @description('Optional. Additional metadata for the connection.')
  metadata: object?

  @description('Optional. Error message if the connection fails.')
  error: string?

  @description('Optional. Expiry time for the connection.')
  expiryTime: string?

  @description('Optional. Private endpoint requirement: Required, NotRequired, or NotApplicable.')
  peRequirement: ('NotApplicable' | 'NotRequired' | 'Required')?

  @description('Optional. Private endpoint status: Active, Inactive, or NotApplicable.')
  peStatus: ('Active' | 'Inactive' | 'NotApplicable')?

  @description('Optional. List of users to share the connection with (alternative to isSharedToAll).')
  sharedUserList: string[]?

  @description('Optional. Whether to use workspace managed identity.')
  useWorkspaceManagedIdentity: bool?

  @description('Optional. OAuth2 authorization endpoint URL (OAuth2 authType only).')
  authorizationUrl: string?

  @description('Optional. OAuth2 token endpoint URL (OAuth2 authType only).')
  tokenUrl: string?

  @description('Optional. OAuth2 refresh token endpoint URL (OAuth2 authType only).')
  refreshUrl: string?

  @description('Optional. OAuth2 scopes to request (OAuth2 authType only).')
  scopes: string[]?

  @description('Optional. Token audience for UserEntraToken / AgenticIdentity auth types.')
  audience: string?

  @description('Optional. Managed connector name for OAuth2 managed connectors.')
  connectorName: string?
}

@description('Connection configuration')
param connectionConfig ConnectionConfig

@secure()
@description('Credentials for the connection (e.g. { key: "..." } for ApiKey, or { clientId: "...", resourceId: "..." } for ManagedIdentity).')
param credentials object = {}

resource foundryAccount 'Microsoft.CognitiveServices/accounts@2026-03-01' existing = {
  name: foundryAccountName

  resource project 'projects' existing = {
    name: foundryProjectName
  }
}

resource connection 'Microsoft.CognitiveServices/accounts/projects/connections@2026-03-01' = {
  parent: foundryAccount::project
  name: connectionConfig.name
  properties: {
    category: connectionConfig.category
    target: connectionConfig.target
    #disable-next-line BCP036
    authType: connectionConfig.authType
    isSharedToAll: connectionConfig.?isSharedToAll ?? true
    credentials: !empty(credentials) ? credentials : null
    metadata: connectionConfig.?metadata
    // Spread optional fields only when present so the ARM payload does not emit nulls.
    ...connectionConfig.?error != null ? { error: connectionConfig.?error } : {}
    ...connectionConfig.?expiryTime != null ? { expiryTime: connectionConfig.?expiryTime } : {}
    ...connectionConfig.?peRequirement != null ? { peRequirement: connectionConfig.?peRequirement } : {}
    ...connectionConfig.?peStatus != null ? { peStatus: connectionConfig.?peStatus } : {}
    ...connectionConfig.?sharedUserList != null ? { sharedUserList: connectionConfig.?sharedUserList } : {}
    ...connectionConfig.?useWorkspaceManagedIdentity != null ? { useWorkspaceManagedIdentity: connectionConfig.?useWorkspaceManagedIdentity } : {}
    ...connectionConfig.?authorizationUrl != null ? { authorizationUrl: connectionConfig.?authorizationUrl } : {}
    ...connectionConfig.?tokenUrl != null ? { tokenUrl: connectionConfig.?tokenUrl } : {}
    ...connectionConfig.?refreshUrl != null ? { refreshUrl: connectionConfig.?refreshUrl } : {}
    ...connectionConfig.?scopes != null ? { scopes: connectionConfig.?scopes } : {}
    ...connectionConfig.?audience != null ? { audience: connectionConfig.?audience } : {}
    ...connectionConfig.?connectorName != null ? { connectorName: connectionConfig.?connectorName } : {}
  }
}

output connectionName string = connection.name
output connectionId string = connection.id
