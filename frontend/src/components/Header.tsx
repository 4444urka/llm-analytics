import React from "react";

export interface IHeaderProps {
  title: React.ReactNode;
  description?: React.ReactNode;
}

const Header: React.FC<IHeaderProps> = ({ title, description }) => {
  return (
    <header className="header">
      <h1>{title}</h1>
      <p>{description}</p>
    </header>
  );
};

export default Header;
