export interface ParsedError {
  original: string
  message: string
  suggestion?: string
  docLink?: string
  type: 'docker' | 'compose' | 'git' | 'network' | 'generic'
}

export function parseError(error: string): ParsedError {
  if (!error) {
    return {
      original: '',
      message: 'Unknown error occurred',
      type: 'generic'
    }
  }

  const errorLower = error.toLowerCase()

  // Docker Compose errors
  if (errorLower.includes('no such file or directory') && errorLower.includes('docker-compose')) {
    return {
      original: error,
      message: 'Compose file not found',
      suggestion: 'Check that the compose_file path is correct in your stack configuration',
      type: 'compose'
    }
  }

  if (errorLower.includes('yaml') || errorLower.includes('syntax error')) {
    return {
      original: error,
      message: 'Invalid YAML syntax in compose file',
      suggestion: 'Validate your docker-compose.yml file syntax. Common issues: incorrect indentation, missing colons, or special characters',
      docLink: 'https://docs.docker.com/compose/compose-file/',
      type: 'compose'
    }
  }

  if (errorLower.includes('no such service') || errorLower.includes('service') && errorLower.includes('not found')) {
    return {
      original: error,
      message: 'Service not found in compose file',
      suggestion: 'Verify the service name exists in your docker-compose.yml',
      type: 'compose'
    }
  }

  if (errorLower.includes('port') && errorLower.includes('already') && errorLower.includes('allocated')) {
    return {
      original: error,
      message: 'Port already in use',
      suggestion: 'Another container or process is using this port. Stop the conflicting service or change the port mapping',
      type: 'docker'
    }
  }

  if (errorLower.includes('image') && (errorLower.includes('not found') || errorLower.includes('pull'))) {
    return {
      original: error,
      message: 'Docker image not found or failed to pull',
      suggestion: 'Check the image name and tag. Verify you have access if using a private registry',
      docLink: 'https://docs.docker.com/engine/reference/commandline/pull/',
      type: 'docker'
    }
  }

  if (errorLower.includes('network') && errorLower.includes('not found')) {
    return {
      original: error,
      message: 'Docker network not found',
      suggestion: 'The network may need to be created. Docker Compose usually creates networks automatically',
      type: 'docker'
    }
  }

  if (errorLower.includes('volume') && errorLower.includes('not found')) {
    return {
      original: error,
      message: 'Docker volume not found',
      suggestion: 'Create the volume manually or let Docker Compose create it automatically',
      type: 'docker'
    }
  }

  // Git errors
  if (errorLower.includes('authentication failed') || errorLower.includes('permission denied')) {
    return {
      original: error,
      message: 'Git authentication failed',
      suggestion: 'Verify your credentials (SSH key, password, or token) are correct and have the necessary permissions',
      type: 'git'
    }
  }

  if (errorLower.includes('repository not found') || errorLower.includes('could not resolve host')) {
    return {
      original: error,
      message: 'Git repository not found or unreachable',
      suggestion: 'Check the repository URL is correct and accessible from your server',
      type: 'git'
    }
  }

  if (errorLower.includes('branch') && errorLower.includes('not found')) {
    return {
      original: error,
      message: 'Git branch not found',
      suggestion: 'Verify the branch name exists in the repository. Default branches are often "main" or "master"',
      type: 'git'
    }
  }

  // Network errors
  if (errorLower.includes('connection refused') || errorLower.includes('dial tcp')) {
    return {
      original: error,
      message: 'Connection refused',
      suggestion: 'The service may not be running or is not accessible. Check firewall rules and network connectivity',
      type: 'network'
    }
  }

  if (errorLower.includes('timeout') || errorLower.includes('timed out')) {
    return {
      original: error,
      message: 'Operation timed out',
      suggestion: 'The operation took too long. Check network connectivity or increase timeout values',
      type: 'network'
    }
  }

  // Generic
  return {
    original: error,
    message: error.split('\n')[0] || 'An error occurred',
    type: 'generic'
  }
}

export function getErrorIcon(type: ParsedError['type']): string {
  switch (type) {
    case 'docker':
      return 'i-lucide-container'
    case 'compose':
      return 'i-lucide-file-code'
    case 'git':
      return 'i-lucide-git-branch'
    case 'network':
      return 'i-lucide-wifi-off'
    default:
      return 'i-lucide-alert-circle'
  }
}

export function getErrorColor(type: ParsedError['type']): string {
  switch (type) {
    case 'network':
      return 'warning'
    default:
      return 'error'
  }
}
