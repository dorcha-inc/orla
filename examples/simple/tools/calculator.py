#!/usr/bin/env python3
# calculator - A simple calculator tool

import sys
import argparse


def main():
    parser = argparse.ArgumentParser(description="Simple calculator")
    parser.add_argument(
        "--operation",
        required=True,
        choices=["add", "subtract", "multiply", "divide"],
        help="Operation to perform",
    )
    parser.add_argument("--a", type=float, required=True, help="First number")
    parser.add_argument("--b", type=float, required=True, help="Second number")

    args = parser.parse_args()

    try:
        if args.operation == "add":
            result = args.a + args.b
        elif args.operation == "subtract":
            result = args.a - args.b
        elif args.operation == "multiply":
            result = args.a * args.b
        elif args.operation == "divide":
            if args.b == 0:
                print("Error: Division by zero", file=sys.stderr)
                sys.exit(1)
            result = args.a / args.b

        print(f"Result: {result}")
        sys.exit(0)
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
