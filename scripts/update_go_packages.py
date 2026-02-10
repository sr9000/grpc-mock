import os
import re

# Determine paths
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
ROOT_DIR = os.path.abspath(os.path.join(SCRIPT_DIR, '..'))
PROTOS_DIR = os.path.join(ROOT_DIR, 'protos')
GO_MODULE_PREFIX = "grpc-mock/internal/genproto"


def get_proto_package(content):
    """Extract the package declaration from proto file content."""
    pattern = r'package\s+([\w\.]+);'
    match = re.search(pattern, content)
    if match:
        return match.group(1)
    return None


def get_go_package_from_proto_package(proto_package):
    """Generate Go package path from proto package declaration."""
    if not proto_package:
        return ""

    # Convert proto package (e.g., "complex.service.models") to path (e.g., "complex/service/models")
    parts = proto_package.split('.')
    # Filter empty parts
    parts = [p for p in parts if p]

    if not parts:
        return ""

    # Determine alias
    last = parts[-1]
    # Check if version (v1, v2, v1beta1, etc)
    if re.match(r'^v\d+', last):
        # Versioned, e.g. store/v1 -> storev1
        if len(parts) > 1:
            alias = parts[-2].replace('_', '') + last
        else:
            alias = last
    else:
        # Not versioned, e.g. echo -> echo, hello_world -> helloworld
        alias = last.replace('_', '')

    # Construct full path
    import_path = f"{GO_MODULE_PREFIX}/{'/'.join(parts)}"
    return f"{import_path};{alias}"


def get_go_package(rel_dir):
    """Generate Go package path from directory structure.

    With paths=source_relative in protoc, all proto files in the same directory
    must have the same go_package based on the directory path.
    """
    # Normalize separators to /
    parts = rel_dir.replace('\\', '/').split('/')
    # Filter empty parts
    parts = [p for p in parts if p]

    if not parts:
        # Root protos folder?
        return ""

    # Determine alias
    last = parts[-1]
    # Check if version (v1, v2, v1beta1, etc)
    if re.match(r'^v\d+', last):
        # Versioned, e.g. store/v1 -> storev1
        if len(parts) > 1:
            alias = parts[-2].replace('_', '') + last
        else:
            alias = last
    else:
        # Not versioned, e.g. echo -> echo, hello_world -> helloworld
        alias = last.replace('_', '')

    # Construct full path
    import_path = f"{GO_MODULE_PREFIX}/{'/'.join(parts)}"
    return f"{import_path};{alias}"

def process_file(file_path):
    with open(file_path, 'r') as f:
        content = f.read()

    rel_path = os.path.relpath(file_path, PROTOS_DIR)
    rel_dir = os.path.dirname(rel_path)

    # Try to extract proto package from file content first
    proto_package = get_proto_package(content)
    if proto_package:
        new_go_package = get_go_package_from_proto_package(proto_package)
    else:
        # Fallback to directory-based package
        new_go_package = get_go_package(rel_dir)

    if not new_go_package:
        print(f"Skipping {rel_path}: in root or cannot determine package")
        return

    print(f"Updating {rel_path} -> {new_go_package}")

    # Regex to find existing go_package
    # option go_package = "...";
    pattern = r'option\s+go_package\s*=\s*".*?";'
    replacement = f'option go_package = "{new_go_package}";'

    if re.search(pattern, content):
        new_content = re.sub(pattern, replacement, content)
    else:
        # Insert it
        # Try to find package declaration
        pkg_pattern = r'(package\s+[\w\.]+;)'
        match = re.search(pkg_pattern, content)
        if match:
            # Insert after package
            new_content = content[:match.end()] + '\n\n' + replacement + content[match.end():]
        else:
            # Insert after syntax if exists
            syntax_pattern = r'(syntax\s*=\s*".*?";)'
            match = re.search(syntax_pattern, content)
            if match:
                new_content = content[:match.end()] + '\n\n' + replacement + content[match.end():]
            else:
                # Just prepend
                new_content = replacement + '\n\n' + content

    # Fix imports to be relative to PROTOS_DIR
    import_pattern = r'(import\s+(?:public\s+|weak\s+)?)"(.*?)"(;\s*)'

    def replace_import(match):
        prefix = match.group(1)
        import_path = match.group(2)
        suffix = match.group(3)

        # 1. Check if it's a local import (relative to current file)
        local_abs_path = os.path.join(os.path.dirname(file_path), import_path)

        if os.path.exists(local_abs_path):
            # It exists locally. Convert to root-relative path.
            root_rel_path = os.path.relpath(local_abs_path, PROTOS_DIR)
            root_rel_path = root_rel_path.replace('\\', '/')

            if root_rel_path != import_path:
                print(f"  Rewriting local import {import_path} -> {root_rel_path}")
                return f'{prefix}"{root_rel_path}"{suffix}'
            return match.group(0)

        # 2. Check if it's already root relative
        root_abs_path = os.path.join(PROTOS_DIR, import_path)
        if os.path.exists(root_abs_path):
             return match.group(0)

        # 3. Check if it is relative to a top-level folder (legacy behavior)
        # e.g. import "v2/models.proto" inside protos/store/v2/admin.proto
        # might resolve to protos/store/v2/models.proto if -I protos/store is used.

        # Iterate top-level folders in PROTOS_DIR
        for item in os.listdir(PROTOS_DIR):
            item_path = os.path.join(PROTOS_DIR, item)
            if os.path.isdir(item_path):
                candidate_path = os.path.join(item_path, import_path)
                if os.path.exists(candidate_path):
                     # Found it!
                     root_rel_path = os.path.relpath(candidate_path, PROTOS_DIR)
                     root_rel_path = root_rel_path.replace('\\', '/')
                     print(f"  Rewriting legacy import {import_path} -> {root_rel_path}")
                     return f'{prefix}"{root_rel_path}"{suffix}'

        # 4. Else, assume it's a system import or missing. Do not touch.
        return match.group(0)

    new_content = re.sub(import_pattern, replace_import, new_content)

    if new_content != content:
        with open(file_path, 'w') as f:
            f.write(new_content)

def main():
    if not os.path.exists(PROTOS_DIR):
        print(f"Protos directory not found: {PROTOS_DIR}")
        return

    for root, dirs, files in os.walk(PROTOS_DIR):
        for file in files:
            if file.endswith(".proto"):
                process_file(os.path.join(root, file))

if __name__ == "__main__":
    main()
